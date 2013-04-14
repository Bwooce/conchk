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
	"fmt"
	"strconv"
	"github.com/droundy/goopt"
	//	"io"
	"encoding/csv"
	"log"
	"net"
	"os"
	"runtime"
	"strings"
	"time"
)

type Parameters struct {
	TestsFile  *string
	MyHost     *string
	MaxStreams *int
}

var params Parameters

type empty struct{}
type semaphore chan empty

var semStreams semaphore

type Test struct {
	desc       string
	net        string
	laddr      string
	raddr      string
	ipv6       bool   // test with underlying AF_INET6 socket
	laddr_used string // the local address that ended up being used for this test
	raddr_used string // the remote address we connected to for this test
	run        bool
	passed     bool
	error      string
}

var TestsToRun []Test

var gotRoot bool

const debug debugging = true // or flip to false

type debugging bool

func (d debugging) Println(args ...interface{}) {
    if d {
        log.Println(append([]interface{}{"DEBUG:"}, args...)...)
    }
}
func (d debugging) Printf(format string, args ...interface{}) {
    if d {
        log.Printf("DEBUG:" + format, args...)
    }
}

func init() {
	goopt.Description = func() string {
		return "conchk v" + goopt.Version + " - (c)2013 Bruce Fitzsimons"
	}
	goopt.Author = "Bruce Fitzsimons <bruce@fitzsimons.org>"
	goopt.Version = "0.1"
	goopt.Summary = "conchk is an IP connectivity test tool. See github.com/Bwooce/conchk for more information."

	Hostname, _ := os.Hostname()

	params.TestsFile = goopt.String([]string{"-T", "--tests"}, "./tests.conchk", "test file to load")
	params.MyHost = goopt.String([]string{"-H", "--host"}, Hostname, "Hostname to use for config lookup")
	params.MaxStreams = goopt.Int([]string{"--maxstreams"}, 8, "Maximum simultaneous checks")

	semStreams = make(semaphore, *params.MaxStreams)
	runtime.GOMAXPROCS(runtime.NumCPU())

}

func main() {
	log.Println("-------------", goopt.Description(), "------------")
	gotRoot = true
	if os.Getuid() != 0 {
		gotRoot = false
		log.Println("WARNING: conchk must run as root to fully function (ICMP listens, low port access etc)")
	}

	// get command line options
	goopt.Parse(nil)

	/*	if !validateOptions() {
		log.Fatal("Incompatible options")
	} */

	// read file or fail doing it
	getTestsFromFile()

	p, inputChan := NewICMPPublisher()
	if(gotRoot) {
		go icmpListen(false, inputChan)
		go icmpListen(true, inputChan)
	}

	// loop over each connection, in a new thread
	for idx, _ := range TestsToRun {
		/*if tt.ipv6 && !net.supportsIPv6 {
					log.Println("IPv6 not supported")
		            continue
		        }*/
		semStreams.acquire(1) // or block until one slot is free
		//fmt.Println("Going to run a goroutine")
		go runTest(&TestsToRun[idx], p)
	}

	var msg ICMPMessage
	inputChan <- msg

	/// try basic check
	/// if that works, try helper app
	/// log success/failures - stderr? syslog? webpage?
	// wait for all to complete
	// end

	debug.Println("going to wait for all goroutines to complete")
	semStreams.acquire(*params.MaxStreams) // don't exit until all goroutines are complete
	debug.Println("all complete")

	log.Println("--------------------- TESTING RUN COMPLETED ---------------------")
	numTests := len(TestsToRun)
	var numPassed int
	for _, test := range TestsToRun {
		log.Println(fmtTest(test))
		if test.passed {
			numPassed++
		}
	}
	log.Printf("== %d of %d tests passed ==", numPassed, numTests)
	log.Println("-------------", goopt.Description(), "-------------")

	if numPassed != numTests {
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
		tests, err := cr.ReadAll()
		if err != nil {
			log.Fatal("Cannot read tests from", *params.TestsFile, "due to error", err)
		}
		//fmt.Println("looking for tests for", *params.MyHost)
		for _, test := range tests {
			if strings.TrimSpace(test[0]) == *params.MyHost {
				appendTest(test)
			}
		}
		
		// See if we can continue or not. Try hard.
		if !gotRoot {
			for _, test := range TestsToRun  {
				if len(test.laddr) > 0 {
					_, aport, err := net.SplitHostPort(test.laddr)
					port, err2 := strconv.ParseUint(aport, 0, 32)
					if err != nil || err2 != nil {
						log.Fatal("Invalid local address on test:",fmtTest(test))
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
	l := len(TestsToRun)
	if l >= cap(TestsToRun) { // reallocate
		// Allocate double what's needed, for future growth.
		newSlice := make([]Test, (l+1)*2)
		// The copy function is predeclared and works for any slice type.
		copy(newSlice, TestsToRun)
		TestsToRun = newSlice
	}
	TestsToRun = TestsToRun[0 : l+1]
	TestsToRun[l].desc = strings.TrimSpace(test[1])
	TestsToRun[l].net = strings.TrimSpace(test[2])

	laddr := strings.TrimSpace(test[3])
	if len(laddr) > 0 {
		i := strings.LastIndex(test[3], ":")
		if i < 0 { // no colon
			laddr += ":0"
		}
	}
	TestsToRun[l].laddr = laddr
	TestsToRun[l].raddr = strings.TrimSpace(test[4])
	debug.Println("size is", len(TestsToRun), "capacity is", cap(TestsToRun))
}

func runTest(test *Test, p *ICMPPublisher) {
	defer semStreams.release(1) // always release, regardless of the reason we exit

	debug.Println("Running test", fmtTest(*test))

	i := strings.LastIndex(test.net, ":")
	var afnet string
	if i < 0 { // no colon
		afnet = test.net
	} else {
		afnet = test.net[:i]
	}
	debug.Println("Got type of", afnet)
	switch afnet {
	//case "ip", "ip4", "ip6":
	case "udp", "udp4", "udp6":
		runUDPTest(afnet, test, p)
	case "tcp", "tcp4", "tcp6":
		runTCPTest(afnet, test, p)
	default:
		// Do ICMP tests since Dial doesn't support them
		test.run = true
		test.error = "Protocol" + afnet + " not yet implemented"
	}
	return
}

func runUDPTest(afnet string, test *Test, p *ICMPPublisher) {
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
	debug.Println("*****Completed: ", fmtTest(*test))
}

func runTCPTest(afnet string, test *Test, p *ICMPPublisher) {
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
	d.Timeout, err = time.ParseDuration("20s")
	conn, err := d.Dial(test.net, test.raddr)
	if err != nil {
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
	debug.Println(fmtTest(*test))
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
	if test.laddr_used != "" {
		local += "(" + test.laddr_used + ")"
	}
	remote := test.raddr
	if test.raddr_used != "" {
		remote += "(" + test.raddr_used + ")"
	}
	status := "PENDING"
	if test.run {
		status = "FAILED"
		if test.passed {
			status = "PASSED"
		}
	}
	out := fmt.Sprintf("%s: '%s' %s %s --> %s", status, pad(test.desc,60), test.net, local, remote)
	if test.ipv6 {
		out += " [on AF_INET6 socket]"
	}
	if len(test.error) > 0 {
		out += " ERROR INFO: " + test.error
	}
	return out
}

func pad(s string, length int) string {
	if(len(s) < length) {
		return s+strings.Repeat(" ", length - len(s))
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

