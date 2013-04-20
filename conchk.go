/*

 Copyright (c) 2013 Bruce Fitzsimons

 This program is free software; you can redistribute it and/or
 modify it under the terms of the GNU General Public License
 as published by the Free Software Foundation; either version 2
 of the License, or (at your option) any later version.

 This program is distributed in the hope that it will be useful,
 but WITHOUT ANY WARRANTY; without even the implied warranty of
 MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 GNU General Public License for more details.

 You should have received a copy of the GNU General Public License
 along with this program; if not, write to the Free Software
 Foundation, Inc., 51 Franklin Street, Fifth Floor, Boston, MA  02110-1301, USA.

*/

package main

import (
	"bytes"
	"container/list"
	"encoding/csv"
	"fmt"
	"github.com/droundy/goopt"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Parameters struct {
	Debug      *bool
	TestsFile  *string
	OutputFile *string
	MyHost     *string
	MaxStreams *int
	Timeout    *string
}

var params Parameters

type empty struct{}
type semaphore chan empty

var semStreams semaphore

// Need to store the original test details so we can write them out again.
// Expanded tests (one per port) are children of this, so we can iterate over them

type Test struct {
	ref      string
	desc     string
	lhost    string
	laddr    string
	ldesc    string
	rhost    string
	raddr    string
	rdesc    string
	net      string
	ipv6     bool // test with underlying AF_INET6 socket
	attempt  bool // should this test be attempted. e.g. does the hostname match?
	run      bool
	passed   bool
	error    string
	subTests list.List // There is always at least one
}

// All subtests must be of the same kind as the parent, but the source and dest addresses/ports can be different.
type SubTest struct {
	subref     string
	laddr      string
	raddr      string
	net        string
	ipv6       bool   // test with underlying AF_INET6 socket
	laddr_used string // the local address that ended up being used for this test
	raddr_used string // the remote address we connected to for this test
	run        bool
	passed     bool
	refused    bool
	error      string
}

var ValidTests uint
var TestsInFile []Test

var gotRoot bool

var debug debugging = !true // or flip to false

type debugging bool

func (d debugging) Println(args ...interface{}) {
	if d {
		log.Println(append([]interface{}{"DEBUG:"}, args...)...)
	}
}
func (d debugging) Printf(format string, args ...interface{}) {
	if d {
		log.Printf("DEBUG:"+format, args...)
	}
}

func init() {
	goopt.Description = func() string {
		return "conchk v" + goopt.Version
	}
	goopt.Author = "Bruce Fitzsimons <bruce@fitzsimons.org>"
	goopt.Version = "0.3"
	goopt.Summary = "conchk is an IP connectivity test tool designed to validate that all configured IP connectivity actually works\n " +
		"It reads a list of tests and executes them, in a parallel manner, based on the contents of each line" +
		"conchk supports tcp and udp based tests (IPv4 and IPv6), at this time.\n\n" +
		"==Notes==\n" +
		"* The incuded Excel sheet is a useful way to create and maintain the tests\n" +
		"* testing a range of supports is supported. In this case the rules for a successful test are somewhat different\n" +
		"** If one of the ports gets a successful connect, and the rest are refused (connection refused) as nothing is listening\n" +
		"\tthen this is considered to be a successful test of the range. This is the most common scenario in our experience;\n" +
		"\tthe firewalls and routing are demonstrably working, and at least one destination service is ok. If you need all ports to work\n" +
		"\tthen consider using individual tests\n" +
		"* If all tests for this host pass, then conchk will exit(0). Otherwise it will exit(1)\n" +
		"* conchk will use the current hostname, or the commandline parameter, to find the tests approprate to execute - matches on field 3.\n" +
		"\tThis means all the tests for a system, or project can be placed in one file\n" +
		"* The .csv output option will write a file much like the input file, but with two additional columns and without any comments\n" +
		"\t This file can be fed back into conchk without error.\n\n" +
		"See http://bwooce.github.io/conchk/ for more information.\n\n(c)2013 Bruce Fitzsimons.\n\n"

	Hostname, _ := os.Hostname()

	params.Debug = goopt.Flag([]string{"-d", "--debug"}, []string{}, "additional debugging output", "")
	params.TestsFile = goopt.String([]string{"-T", "--tests"}, "./tests.conchk", "test file to load")
	params.OutputFile = goopt.String([]string{"-O", "--outputcsv"}, "", "name of results .csv file to write to. A pre-existing file will be overwritten.")
	params.MyHost = goopt.String([]string{"-H", "--host"}, Hostname, "Hostname to use for config lookup")
	params.MaxStreams = goopt.Int([]string{"--maxstreams"}, 8, "Maximum simultaneous checks")
	params.Timeout = goopt.String([]string{"--timeout"}, "5s", "TCP connectivity timeout, UDP delay for ICMP responses")

	semStreams = make(semaphore, *params.MaxStreams)
	runtime.GOMAXPROCS(runtime.NumCPU())

}

func main() {
	log.Println("--------------------------", "conchk v"+goopt.Version, "--------------------------")
	gotRoot = true
	if os.Getuid() != 0 {
		gotRoot = false
		log.Println("WARNING: conchk must run as root for full functionality (ICMP listens, low port access etc)")
	}

	// get command line options
	goopt.Parse(nil)
	debug = debugging(*params.Debug)

	/*	if !validateOptions() {
		log.Fatal("Incompatible options")
	} */

	// read file or fail doing it
	getTestsFromFile()

	p, inputChan := NewICMPPublisher()
	if gotRoot {
		go icmpListen(false, inputChan)
		go icmpListen(true, inputChan)
	}

	// loop over each connection, in a new thread
	for idx, _ := range TestsInFile {
		/*if tt.ipv6 && !net.supportsIPv6 {
					log.Println("IPv6 not supported")
		            continue
		        }*/
		if TestsInFile[idx].attempt {
			semStreams.acquire(1) // or block until one slot is free
			//fmt.Println("Going to run a goroutine")
			go runTest(&TestsInFile[idx], p)
		}
	}

	debug.Println("going to wait for all goroutines to complete")
	semStreams.acquire(*params.MaxStreams) // don't exit until all goroutines are complete
	debug.Println("all complete")

	log.Println("--------------------- TESTING RUN COMPLETED ---------------------")
	var numPassed uint
	for _, test := range TestsInFile {
		if test.attempt {
			log.Println(fmtTest(test))
			if test.passed {
				numPassed++
			}
		}
	}
	log.Printf("== %d of %d tests passed ==", numPassed, ValidTests)
	log.Println("--------------------------", goopt.Description(), "--------------------------")

	if *params.OutputFile != "" {
		const hdr = "#format is: TestRef,TestDescription,Hostname,LocalIP:Port,LocalDescription[u],RemoteHost[u],RemoteIP:Port,RemoteDescription[u],Protocol(tcp,udp,tcp4 etc),Result[o],Summary[o]\n# [u] fields are currently unused, [o] are optional\n"
		buf := bytes.NewBufferString(hdr)
		fd, err := os.OpenFile(*params.OutputFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0655)
		if err == nil {
			defer fd.Close()
			fd.Write(buf.Bytes())
			w := csv.NewWriter(fd)
			for _, test := range TestsInFile {
				l := fmtTestCSV(test)
				w.Write(l)
			}
			w.Flush()
		} else {
			log.Printf("Cannot open file %s due to error %s: skipping and exiting with error", *params.OutputFile, err)
			os.Exit(1)
		}
	}

	if numPassed != ValidTests {
		os.Exit(1) // indicate an error
	}
	os.Exit(0)
}

func getTestsFromFile() {
	log.Println("Reading tests for", *params.MyHost, "from file", *params.TestsFile)
	if params.TestsFile != nil {
		file, err := os.Open(*params.TestsFile)
		if err != nil {
			log.Fatal("Cannot open", *params.TestsFile, "due to error", err)
		}
		cr := csv.NewReader(file)
		cr.Comment = '#'

		tests, err := cr.ReadAll()
		if err != nil {
			log.Fatal("Cannot read tests from", *params.TestsFile, "due to error", err)
		}
		//fmt.Println("looking for tests for", *params.MyHost)
		for _, test := range tests {
			appendTest(test)
		}

		// See if we can continue or not. Try hard.
		if !gotRoot {
			for _, test := range TestsInFile {
				if test.attempt && len(test.laddr) > 0 {
					_, aport, err := net.SplitHostPort(test.laddr)
					port, err2 := strconv.ParseUint(aport, 0, 32)
					if err != nil || err2 != nil {
						log.Fatal("Invalid local address on test:", fmtTest(test))
					}
					if port > 0 && port < 1024 {
						log.Fatal("Cannot execute test w/o root access:", fmtTest(test))
					}
				}
			}
		}

		return
	}
}

func appendTest(test []string) {

	var newTest Test

	newTest.lhost = strings.TrimSpace(test[2])
	if newTest.lhost == *params.MyHost {
		newTest.attempt = true
		ValidTests++
	}
	newTest.ref = strings.TrimSpace(test[0])
	newTest.desc = strings.TrimSpace(test[1])
	laddr := strings.TrimSpace(test[3])
	if len(laddr) > 0 {
		i := strings.LastIndex(test[3], ":")
		if i < 0 { // no colon
			laddr += ":0"
		}
	}
	newTest.laddr = laddr
	newTest.ldesc = strings.TrimSpace(test[4])
	newTest.rhost = strings.TrimSpace(test[5])
	newTest.raddr = strings.TrimSpace(test[6])
	newTest.ldesc = strings.TrimSpace(test[7])
	newTest.net = strings.TrimSpace(test[8])

	address, startPort, endPort := findDestRange(newTest.raddr)
	debug.Printf("iterating from %d to %d", startPort, endPort+1)

	for cnt := 0; cnt < (endPort-startPort)+1; cnt++ {
		debug.Printf("Adding test for dest port %d", startPort+cnt)
		var newSubTest SubTest

		if startPort != endPort && startPort != 0 {
			newSubTest.subref = fmt.Sprintf("%s.%d", newTest.ref, cnt+1)
		}

		// these three are not currently mutable per subTest, but are convenient to have here
		newSubTest.net = newTest.net
		newSubTest.ipv6 = newTest.ipv6
		newSubTest.laddr = newTest.laddr

		if startPort == 0 {
			newSubTest.raddr = address
		} else {
			newSubTest.raddr = fmt.Sprintf("%s:%d", address, startPort+cnt)
		}

		newTest.subTests.PushBack(&newSubTest)
	}

	l := len(TestsInFile)
	if l+1 > cap(TestsInFile) { // reallocate
		// Allocate double what's needed, for future growth.
		newSlice := make([]Test, l+1, (l+1)*2)
		// The copy function is predeclared and works for any slice type.
		copy(newSlice, TestsInFile)
		TestsInFile = newSlice
	}
	TestsInFile = TestsInFile[0 : l+1]
	TestsInFile[l] = newTest
	debug.Printf("TestsInFile l=%d, len is %d, cap is %d", l, len(TestsInFile), cap(TestsInFile))
}

// Find the range of ports on the destination, if any. Returns 0,0 for error which should work fine (will error out on non-IP or ICMP protos)
// port range is inclusive -- we need to test the start and the end
func findDestRange(IPPort string) (IP string, startPort, endPort int) {
	i := strings.LastIndex(IPPort, ":")
	if i < 0 { // no colon
		debug.Println("No colon")
		return IPPort, 0, 0
	}
	j := strings.LastIndex(IPPort[i+1:], "-")
	if j < 0 { // no range
		debug.Println("solo port:", IPPort[i+1:])
		startPort, err := strconv.ParseInt(IPPort[i+1:], 0, 32)
		if err != nil {
			return IPPort, 0, 0 // yeah, but this will have to do
		}
		return IPPort[:i], int(startPort), int(startPort)
	}
	debug.Printf("hyphen pos: %d, len %d", j, len(IPPort))
	debug.Println("start:", IPPort[i+1:i+j+1])
	port, err := strconv.ParseInt(IPPort[i+1:i+j+1], 0, 32)
	startPort = int(port)
	if err != nil {
		return IPPort, 0, 0 // yeah, but this will have to do
	}
	debug.Println("end:", IPPort[i+j+1:])
	port, err = strconv.ParseInt(IPPort[i+j+2:], 0, 32)
	endPort = int(port)
	if err != nil {
		return IPPort, 0, 0 // yeah, but this will have to do
	}
	// Stop the madness before it begins
	if endPort < startPort {
		return IPPort, 0, 0
	}
	IP = IPPort[:i]
	return
}

func runTest(test *Test, p *ICMPPublisher) {
	defer semStreams.release(1) // always release, regardless of the reason we exit

	debug.Println("Running test", fmtTest(*test))

	test.run = true
	allPassed := true
	var errorText string

	i := strings.LastIndex(test.net, ":")
	var afnet string
	if i < 0 { // no colon
		afnet = test.net
	} else {
		afnet = test.net[:i]
	}
	debug.Println("Got type of", afnet)
	for subTestV := test.subTests.Front(); subTestV != nil; subTestV = subTestV.Next() {
		subTest := subTestV.Value.(*SubTest)
		switch afnet {
		//case "ip", "ip4", "ip6":
		case "udp", "udp4", "udp6":
			runUDPTest(afnet, subTest, p)
		case "tcp", "tcp4", "tcp6":
			runTCPTest(afnet, subTest, p)
		default:
			// Do ICMP tests since Dial doesn't support them
			allPassed = false
			errorText = "Protocol" + afnet + " not yet implemented"
		}
	}

	// Rules for test passing.
	// If there is only one test, then it must connect.
	// If there is a range, then 1->all of them must connect, but some are allowed to be refused
	// If there is a range and any fail for another reason, then the test fails.
	listLen := test.subTests.Len()
	onePass := false
	for subTestV := test.subTests.Front(); subTestV != nil; subTestV = subTestV.Next() {
		subTest := subTestV.Value.(*SubTest)
		// record if one test passed
		if subTest.passed && !onePass {
			onePass = true
		}
		if !subTest.passed && listLen == 1 {
			allPassed = false
			errorText = subTest.error
		}

		if listLen > 1 && !subTest.passed && !subTest.refused {
			debug.Printf("subTest %+v", *subTest)
			errorText += fmt.Sprintf("%s %s;", subTest.subref, subTest.error)
			if allPassed {
				allPassed = false
			}
		}
	}

	// Cater for the case where all ports were refused
	if allPassed && !onePass {
		allPassed = false
	}
	test.passed = allPassed
	test.error = errorText

	return
}

func runUDPTest(afnet string, test *SubTest, p *ICMPPublisher) {
	debug.Println("Doing UDP test")

	var icmpCh chan ICMPMessage
	if gotRoot {
		icmpCh = p.Subscribe()
		defer p.Unsubscribe(icmpCh)
	}

	var d net.Dialer
	var err error

	d.LocalAddr, err = net.ResolveUDPAddr(afnet, test.laddr)
	if err != nil {
		test.run = true
		test.error = "UDP Resolve error: " + err.Error()
		return
	}

	d.Timeout, err = time.ParseDuration("20s")
	conn, err := d.Dial(test.net, test.raddr)
	if err != nil {
		test.run = true
		test.error = "UDP Dial error: " + err.Error()
		return
	}
	test.laddr_used = conn.LocalAddr().String()
	test.raddr_used = conn.RemoteAddr().String()
	_, err = conn.Write([]byte("conchk test packet")) // hard to fail for UDP, the ICMP response is the important thing
	if err != nil {
		test.run = true
		test.error = "UDP Write error: " + err.Error()
		return
	}
	conn.Close()

	if gotRoot && waitForPossibleICMP(test, icmpCh) {
		debug.Println("Failed due to ICMP response")
	} else {
		test.run = true
		test.passed = true
	}
	debug.Println("*****Completed: ", fmtSubTest(*test))
}

func runTCPTest(afnet string, test *SubTest, p *ICMPPublisher) {
	debug.Println("Doing TCP test")
	var d net.Dialer
	var err error

	// no need to register for ICMP as the TCP RST will be enough for us

	d.LocalAddr, err = net.ResolveTCPAddr(afnet, test.laddr)
	if err != nil {
		test.run = true
		test.error = "TCP Resolve error: " + err.Error()
		return
	}
	d.Timeout, err = time.ParseDuration(*params.Timeout)
	if err != nil {
		test.run = true
		test.error = "Invalid timeout value specified: " + err.Error()
		return
	}
	conn, err := d.Dial(test.net, test.raddr)
	if nerr, ok := err.(*net.OpError); ok && nerr.Err.Error() == "connection refused" {
		test.run = true
		test.refused = true
		debug.Printf("Got TCP conn refused %v....%+v", nerr, *nerr)
		return
	}
	if err != nil {
		debug.Printf("Error was type %T, %+v", err, err)
		test.run = true
		test.error = "Connect error: " + err.Error()
		return
	}
	test.laddr_used = conn.LocalAddr().String()
	test.raddr_used = conn.RemoteAddr().String()
	_, err = conn.Write([]byte("conchk test packet"))
	if err != nil {
		test.run = true
		test.error = "TCP Write error: " + err.Error()
		return
	}
	conn.Close()
	test.run = true
	test.passed = true
	debug.Println(fmtSubTest(*test))
}

// acquire n resources
func (s semaphore) acquire(n int) {
	e := empty{}
	for i := 0; i < n; i++ {
		s <- e
	}
}

// release n resources
func (s semaphore) release(n int) {
	for i := 0; i < n; i++ {
		<-s
	}
}

func fmtTest(test Test) string {
	var local string
	if test.laddr == "" {
		local = "#any#"
	} else {
		local = test.laddr
	}

	status := testResult(test)

	out := fmt.Sprintf("%s: %s '%s' %s %s --> %s", status, pad(test.ref, 5), pad(test.desc, 60), test.net, local, test.raddr)
	if test.ipv6 {
		out += " [on AF_INET6 socket]"
	}
	if len(test.error) > 0 {
		out += " ERROR INFO: " + test.error
	}
	return out
}

func fmtSubTest(test SubTest) string {

	// only print the addresses actually used, since the parent Test will print the requested values
	status := subTestResult(test)

	out := fmt.Sprintf("%s %s --> %s %s", pad(test.subref, 6), test.laddr_used, test.raddr_used, status)
	if len(test.error) > 0 {
		out += " ERROR INFO: " + test.error
	}
	return out
}

func testResult(test Test) string {
	status := "PENDING"
	if test.run {
		status = "FAILED"
		if test.passed {
			status = "PASSED"
		}
	}
	return status
}

func subTestResult(test SubTest) string {
	status := "PENDING"
	if test.run {
		status = "FAILED"
		if test.passed {
			status = "PASSED"
		}
	}
	return status
}

func fmtTestCSV(test Test) []string {
	const fields int = 11

	out := make([]string, fields)

	out = out[0:fields]
	out[0] = test.ref
	out[1] = test.desc
	out[2] = test.lhost
	out[3] = test.laddr
	out[4] = test.ldesc
	out[5] = test.rhost
	out[6] = test.raddr
	out[7] = test.rdesc
	out[8] = test.net
	out[9] = testResult(test)
	out[10] = test.error

	debug.Printf("CSV line is %v", out)
	return out
}

func pad(s string, length int) string {
	if len(s) < length {
		return s + strings.Repeat(" ", length-len(s))
	}
	return s
}

// Huge assumption that IPv6 addresses will always be in square brackets. Lets run with this for now
// TODO(bf) Verify this, or validate it on config read at least
func isV6(ip string) bool {
	if strings.LastIndex(ip, "[") < 0 {
		return false
	}
	return true
}
