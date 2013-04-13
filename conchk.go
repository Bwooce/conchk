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
	"github.com/droundy/goopt"
	"fmt"
//	"io"
	"strings"
	"time"
	"net"
	"log"
	"os"
	"runtime"
)

type Parameters struct {
	ConfigFile  *string
	MyHost      *string
	MaxStreams  *int
}

var params Parameters

type empty struct{}
type semaphore chan empty

var semStreams semaphore

var defaultTests = []Test {
    {"ICMPv4 localhost", "ip4:icmp", "", "127.0.0.1", false, "", "", false, false},
    {"ICMPv6 localhost", "ip6:icmp", "", "::1", true, "", "", false, false},
	{"UDP localhost:80", "udp4", "localhost:1025", "127.0.0.1:80", false, "", "", false, false},
	{"TCP localhost:http[80]", "tcp4", "", "127.0.0.1:80", false, "", "", false, false},
	{"TCP bad.example.com:http[80]", "tcp4", "", "bad.example.com:http", false, "", "", false, false},
}

type Test struct {
	desc  string
    net   string
    laddr string
    raddr string
    ipv6  bool // test with underlying AF_INET6 socket
	laddr_used string // the local address that ended up being used for this test
	raddr_used string // the remote address we connected to for this test
	run    bool
	passed bool
}



func init() {
	goopt.Description = func() string {
		return "conchk (c) 2013 Bruce Fitzsimons"
	}
	goopt.Author = "Bruce Fitzsimons <bruce@fitzsimons.org>"
	goopt.Version = "0.1"
	goopt.Summary = "conchk is an ip connectivity test tool"

	Hostname, _ := os.Hostname()

	params.ConfigFile = goopt.String([]string{"-C", "--config"}, "~/conchk.cfg", "Config file to load")
	params.MyHost = goopt.String([]string{"-H", "--host"}, Hostname, "Hostname to use for config lookup")
	params.MaxStreams = goopt.Int([]string{"--maxstreams"}, 8, "Maximum simultaneous checks")
	
	semStreams = make(semaphore, *params.MaxStreams)
	runtime.GOMAXPROCS(runtime.NumCPU())

}


func main() {
    if os.Getuid() != 0 {
        log.Println("test disabled; must be root")
        return
    }

// get command line options
	goopt.Parse(nil)

/*	if !validateOptions() {
		log.Fatal("Incompatible options")
	} */

// read file


	p, inputChan := NewICMPPublisher()

// loop over each connection, in a new thread
    for idx, _ := range defaultTests {
        /*if tt.ipv6 && !net.supportsIPv6 {
			log.Println("IPv6 not supported")
            continue
        }*/
		semStreams.acquire(1) // or block until one slot is free
		fmt.Println("Going to run a goroutine")
		go runTest(&defaultTests[idx],p)
    }

	var msg ICMPMessage
	inputChan<-msg

/// try basic check
/// if that works, try helper app
/// log success/failures - stderr? syslog? webpage?
// wait for all to complete
// end

	fmt.Println("going to wait for all goroutines to complete")
	semStreams.acquire(*params.MaxStreams) // don't exit until all goroutines are complete
	fmt.Println("all complete")

}

func getConfig() {

}

func runTest(test *Test, p *ICMPPublisher) (error) {
	defer semStreams.release(1) // always release, regardless of the reason we exit
	
	icmpCh := p.Subscribe()
	defer p.Unsubscribe(icmpCh)

	fmt.Println("Running test", fmtTest(*test))

	i := strings.LastIndex(test.net, ":")
	var afnet string
    if i < 0 { // no colon
		afnet = test.net
    } else {
		afnet = test.net[:i]
	}
	fmt.Println("Got type of" , afnet)
    switch afnet {
	case "ip", "ip4","ip6":
		// Do ICMP tests since Dial doesn't support them
		test.run = true
		log.Println(fmtTest(*test), "IP not yet implemented")
		return nil
	default:
		fmt.Println("Doing UDP/TCP test")
		var d net.Dialer
		var err error
		switch afnet {
		case "tcp", "tcp4", "tcp6":
			d.LocalAddr, err = net.ResolveTCPAddr(afnet, test.laddr)
			if err != nil {
				test.run = true
				log.Println(fmtTest(*test), "TCP Resolve error:", err)
    			return err
			}
		default:
			d.LocalAddr, err = net.ResolveUDPAddr("udp", test.laddr)
			if err != nil {
				test.run = true
				log.Println(fmtTest(*test), "had UDP Resolve error:", err)
    			return err
			}
		}		
		d.Timeout, err = time.ParseDuration("20s")
		conn, error := d.Dial(test.net, test.raddr)
		if error != nil {
			test.run = true
			log.Println(fmtTest(*test), "had Dial error:", error)
			return error
		}
		test.laddr_used = conn.LocalAddr().String()
		test.raddr_used = conn.RemoteAddr().String()
		_, err = conn.Write([]byte("conchk test packet"))
		if err != nil {
			test.run = true
			log.Println(fmtTest(*test), "Write error:", err)
    		return err
		}
		conn.Close()
		test.run = true
		test.passed = true
		log.Println(fmtTest(*test))

		return nil
	}
	return nil


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


func fmtTest(test Test) (string) {
	var local string
	if test.laddr == "" {
			local = "#undefined#"
	} else {
		local = test.laddr
	}
	if test.laddr_used != "" {
		local += "("+test.laddr_used+")"
	}
	remote := test.raddr
	if test.raddr_used != "" {
		remote += "("+test.raddr_used+")"
	}
	status := "PENDING"
	if test.run {
		status = "FAILED"
		if test.passed {
			status = "PASSED"
		}
	}
	out := fmt.Sprintf("%s: '%s' %s %s --> %s", status, test.desc, test.net, local, remote)
	if test.ipv6  {
		out += " [on AF_INET6 socket]"
	}
		return out
}

// Huge assumption that IPv6 addresses will always be in square brackets. Lets run with this for now
// TODO(bf) Verify this, or validate it on config read at least
func isV6(ip string)  (bool) {
	if strings.LastIndex(ip, "[") > 0 {
		return true 
	}
	return false
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


