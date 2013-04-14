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
	if os.Getuid() != 0 {
		log.Println("conchk must run as root to function (ICMP listens, low port access etc)")
		return
	}

	// get command line options
	goopt.Parse(nil)

	/*	if !validateOptions() {
		log.Fatal("Incompatible options")
	} */

	// read file or fail doing it
	getTestsFromFile()

	p, inputChan := NewICMPPublisher()

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

	//fmt.Println("going to wait for all goroutines to complete")
	semStreams.acquire(*params.MaxStreams) // don't exit until all goroutines are complete
	//fmt.Println("all complete")

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
}

func runTest(test *Test, p *ICMPPublisher) {
	defer semStreams.release(1) // always release, regardless of the reason we exit

	//fmt.Println("Running test", fmtTest(*test))

	i := strings.LastIndex(test.net, ":")
	var afnet string
	if i < 0 { // no colon
		afnet = test.net
	} else {
		afnet = test.net[:i]
	}
	//fmt.Println("Got type of", afnet)
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
	//fmt.Println("Doing UDP test")

	icmpCh := p.Subscribe()
	defer p.Unsubscribe(icmpCh)

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
	_, err = conn.Write([]byte("conchk test packet")) // almost impossible to fail for UDP, the ICMP response is the important thing
	if err != nil {
		test.run = true
		test.error = "UDP Write error: " + err.Error()
		return
	}
	conn.Close()
	test.run = true
	test.passed = true
	//log.Println(fmtTest(*test))
}

func runTCPTest(afnet string, test *Test, p *ICMPPublisher) {
	//fmt.Println("Doing TCP test")
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
	//log.Println(fmtTest(*test))
}

/*
func icmpTest(net string, laddr string, raddr string) {
	err := net.ListenPacket(net, laddr)
    if err != nil {
    	log.Println("ListenPacket(%q, %q) failed: %v", net, laddr, err)
    	return
    }
    c.SetDeadline(time.Now().Add(100 * time.Millisecond))
   	defer c.Close()

 	ra, err := ResolveIPAddr(net, raddr)
    if err != nil {
    	log.Println("ResolveIPAddr(%q, %q) failed: %v", net, raddr, err)
    	return
    }

    waitForReady := make(chan bool)
    go icmpEchoTransponder(t, net, raddr, waitForReady)
    <-waitForReady

    _, err = c.WriteTo(echo, ra)
    if err != nil {
    	log.Println("WriteTo failed: %v", err)
    	return
    }

    reply := make([]byte, 256)
    for {
    	_, _, err := c.ReadFrom(reply)
    	if err != nil {
    		log.Println("ReadFrom failed: %v", err)
    		return
    	}
    	switch c.(*IPConn).fd.family {
    	case syscall.AF_INET:
    		if reply[0] != ICMP4_ECHO_REPLY {
    			continue
			}
    	case syscall.AF_INET6:
    		if reply[0] != ICMP6_ECHO_REPLY {
    			continue
    		}
    	}
    	xid, xseqnum := parseICMPEchoReply(echo)
    	rid, rseqnum := parseICMPEchoReply(reply)
    	if rid != xid || rseqnum != xseqnum {
    		log.Println("ID = %v, Seqnum = %v, want ID = %v, Seqnum = %v", rid, rseqnum, xid, xseqnum)
    		return
    	}
   		break
    }

}
*/

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
		local = "#undefined#"
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

/*
func parseConnType(network string) (afnet string, proto int, err error) {
    i := last(network, ':')
    if i < 0 { // no colon
    	switch network {
    	case "tcp", "tcp4", "tcp6":
		case "udp", "udp4", "udp6":
    	case "unix", "unixgram", "unixpacket":
    	default:
    		return "", 0, net.UnknownNetworkError(net)
    	}
    	return net, 0, nil
    }
    afnet = network[:i]
    switch afnet {
    case "ip", "ip4", "ip6":
    	protostr := network[i+1:]
    	proto, i, ok := dtoi(protostr, 0)
    	if !ok || i != len(protostr) {
			proto, err = lookupProtocol(protostr)
			if err != nil {
    			return "", 0, err
    		}
    	}
    	return afnet, proto, nil
    }
    return "", 0, net.UnknownNetworkError(net)
}

func resolveAddr(afnet, addr string) (a Addr, err error) {
	switch afnet {
    case "tcp", "tcp4", "tcp6":
    	if addr != "" {
    		a, err = net.ResolveTCPAddr(afnet, addr)
    	}
    case "udp", "udp4", "udp6":
    	if addr != "" {
    		a, err = net.ResolveUDPAddr(afnet, addr)
    	}
    case "ip", "ip4", "ip6":
    	if addr != "" {
    		a, err = net.ResolveIPAddr(afnet, addr)
    	}
	}
}

*/
