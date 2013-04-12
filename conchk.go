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
//	"fmt"
//	"io"
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
        {"ip4:icmp", "", "127.0.0.1", false},
        {"ip6:icmp", "", "::1", true},
}

type Test struct {
        net   string
        laddr string
        raddr string
        ipv6  bool // test with underlying AF_INET6 socket
}

func init() {
	goopt.Description = func() string {
		return "conchk (c) 2013 Bruce Fitzsimons"
	}
	goopt.Author = "Bruce Fitzsimons <bruce@fitzsimons.org>"
	goopt.Version = "0.1"
	goopt.Summary = "conchk is an ip connectivity test tool"

	Hostname, error := os.Hostname()

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

// loop over each connection, in a new thread
    for _, tt := range defaultTests {
        /*if tt.ipv6 && !net.supportsIPv6 {
			log.Println("IPv6 not supported")
            continue
        }*/
		semStreams.acquire(1) // or block until one slot is free
		log.Println("Going to run a goroutine")
		go runTest(tt)
    }

/// try basic check
/// if that works, try helper app
/// log success/failures - stderr? syslog? webpage?
// wait for all to complete
// end

	log.Println("going to wait for all goroutines to complete")
	semStreams.acquire(*params.MaxStreams) // don't exit until all goroutines are complete
	log.Println("all complete")

}

func getConfig() {

}

func runTest(test Test) {
	afnet, proto, err := parseConnType(test.net)
    switch afnet {
    case "tcp", "tcp4", "tcp6":
		//tcpConn(test.net, test.laddr, test.raddr)
	case "udp", "udp4", "udp6":	
		udpTest(test.net, test.laddr, test.raddr)
	case "ip", "ipv4", "ipv6":
		ipTest(test.net, test.laddr, test.raddr)
	}
	return
}

// pure IP test. Can puport to carry any protocol, in reality carries nothing.
func ipTest(network string, laddr string, raddr string) (failed bool, err error) {
	cl, err := net.ListenPacket(network, laddr)
    if err != nil {
    	log.Println("ListenPacket(%q, %q) failed: %v", network, laddr, err)
    	return true, err
    }
    cl.SetDeadline(time.Now().Add(100 * time.Millisecond))
   	defer cl.Close()

	_, raddri, err := parseConnType(network, raddr)
    if err != nil {
    	return true, err
    }
	_, laddri, err := resolveNetAddr("listen", network, laddr)
    if err != nil {
    	return true, err
    }
	cr, err = DialIP(network, laddri, raddri)
	if err != nil {
    	return true, err
    }
	_, err = cr.Write([]byte("conchk test packet"))
	if err != nil {
    	return true, err
    }
	cr.Close()

    reply := make([]byte, 256)
    for {
        _, _, err := cl.ReadFrom(reply)
        if err != nil {
            t.Errorf("ReadFrom failed: %v", err)
            return
        }
        switch cl.(*IPConn).fd.family {
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
            t.Errorf("ID = %v, Seqnum = %v, want ID = %v, Seqnum = %v", rid, rseqnum, xid, xseqnum)
            return
        }
        break
    }

	return false, err
}

func tcpConnect(net string, laddr string, raddr string, maxWait int) {
	conn, err := net.Dial("tcp", "google.com:80")
	if err != nil {
		// handle error
	}
}

func udpTest(net string, laddr string, raddr string) {
	err := ListenPacket(net, laddr)
    if err != nil {
    	t.Errorf("ListenPacket(%q, %q) failed: %v", net, laddr, err)
    	return
    }
    c.SetDeadline(time.Now().Add(100 * time.Millisecond))
   	defer c.Close()

}

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


