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
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

type ICMPMessage struct {
	msgtype       int8
	code          int8
	desc          string
	originalRAddr string
	originalLAddr string
	originalProto string
}

type ICMPPublisher struct {
	subs  map[chan ICMPMessage]bool
	sub   chan chan ICMPMessage
	unsub chan chan ICMPMessage
}

const ChanDepth = 10

const (
	ICMP4_ECHO_REQUEST      = 8
	ICMP4_ECHO_REPLY        = 0
	ICMP4_DEST_UNREACHABLE  = 3
	ICMP4_TIME_EXCEEDED     = 11
	ICMP4_PARAMETER_PROBLEM = 12

	ICMP6_DEST_UNREACHABLE  = 1
	ICMP6_PACKET_TOO_BIG    = 2
	ICMP6_TIME_EXCEEDED     = 3
	ICMP6_PARAMETER_PROBLEM = 4
	ICMP6_ECHO_REQUEST      = 128
	ICMP6_ECHO_REPLY        = 129
)

// ICMP4_PARAMETER_PROBLEM
var ICMP4ParameterProblemCodes map[int8]string = map[int8]string{
	0: "Pointer indicates the error", // TODO(bf) this needs some more work to decode the pointer
	1: "Missing a required option",
	2: "Bad length",
}

// ICMP4_TIME_EXCEEDED codes
var ICMP4TimeExceededCodes map[int8]string = map[int8]string{
	0: "Time-to-live exceeded in transit",
	1: "Fragment reassembly time exceeded",
}

// ICMP4_DEST_UNREACHABLE codes
var ICMP4UnreachableCodes map[int8]string = map[int8]string{
	0:  "Network unreachable error",
	1:  "Host unreachable error",
	2:  "Protocol unreachable error (the designated transport protocol is not supported)",
	3:  "Port unreachable error (the designated protocol is unable to inform the host of the incoming message)",
	4:  "The datagram is too big. Packet fragmentation is required but the 'don't fragment' (DF) flag is on.",
	5:  "Source route failed error",
	6:  "Destination network unknown error",
	7:  "Destination host unknown error",
	8:  "Source host isolated error",
	9:  "The destination network is administratively prohibited",
	10: "The destination host is administratively prohibited",
	11: "The network is unreachable for Type Of Service",
	12: "The host is unreachable for Type Of Service",
	13: "Communication administratively prohibited (administrative filtering prevents packet from being forwarded)",
	14: "Host precedence violation (indicates the requested precedence is not permitted for the combination of host or network and port)",
	15: "Precedence cutoff in effect (precedence of datagram is below the level set by the network administrators)",
}

var IPProtocol map[uint8]string = map[uint8]string{
	1:   "ICMP",
	2:   "IGMP",
	6:   "TCP",
	17:  "UDP",
	41:  "ENCAP",
	89:  "OSPF",
	132: "SCTP",
}

func NewICMPPublisher() (*ICMPPublisher, chan ICMPMessage) {
	p := &ICMPPublisher{
		subs:  make(map[chan ICMPMessage]bool),
		sub:   make(chan chan ICMPMessage),
		unsub: make(chan chan ICMPMessage),
	}
	values := make(chan ICMPMessage)
	go func() {
		next := make(chan ICMPMessage, ChanDepth)
		for {
			select {
			case v := <-values:
				for c := range p.subs {
					select {
					case c <- v:
					default:
					}
				}
			case p.sub <- next:
				p.subs[next], next = true, make(chan ICMPMessage, ChanDepth)
			case c := <-p.unsub:
				delete(p.subs, c)
			}
		}
	}()
	return p, values
}

func (p *ICMPPublisher) Subscribe() chan ICMPMessage {
	return <-p.sub
}

func (p *ICMPPublisher) Unsubscribe(ch chan ICMPMessage) {
	p.unsub <- ch
}

func waitForPossibleICMP(test *SubTest, icmpCh chan ICMPMessage) bool {
	timeout := make(chan bool, 1)
	go func() {
		dur, err := time.ParseDuration(*params.Timeout)
		if err == nil {
			time.Sleep(dur)
		}
		timeout <- true
	}()

	for {
		select {
		case msg := <-icmpCh:
			match, err := matchICMP(test, msg)
			if match {
				test.run = true
				// any matching ICMP message is bad and invalidates the test, unless it's unreachable
				test.passed = false
				test.refused = false
				if msg.msgtype == ICMP4_DEST_UNREACHABLE || msg.msgtype == ICMP6_DEST_UNREACHABLE {
					test.refused = true
				}
				test.error = err.Error()
				debug.Println(fmtSubTest(*test), "ICMP", err)
				return true
			}
		case <-timeout: // this is the normal case
			debug.Println("Timeout - exiting loop")
			return false
		}
	}
}

// TODO(bf) IPv6. Enough said.
func matchICMP(test *SubTest, msg ICMPMessage) (bool, error) {
	debug.Printf("Matching message %v to a test", msg)
	if test.laddr_used == msg.originalLAddr && test.raddr_used == msg.originalRAddr {
		debug.Printf("Proto %s vs %s", strings.ToLower(msg.originalProto), test.net[:len(msg.originalProto)])
		if strings.ToLower(msg.originalProto) == test.net[:len(msg.originalProto)] {
			debug.Println("#################MATCH#############")
			return true, errors.New(msg.desc)
		}
	}
	return false, nil
}

func parseICMPEchoReply(b []byte) (id, seqnum int) {
	id = int(b[4])<<8 | int(b[5])
	seqnum = int(b[6])<<8 | int(b[7])
	return
}

func icmpListen(v6 bool, ch chan ICMPMessage) {
	afnet := "ip4:icmp"
	if v6 {
		afnet = "ip6:ipv6-icmp"
	}

	c, err := net.ListenPacket(afnet, "")
	if err != nil {
		debug.Printf("ListenPacket failed: %v", err)
		return
	}
	defer c.Close()

	rawICMP := make([]byte, 256)
	for {
		_, fromAddr, err := c.ReadFrom(rawICMP)
		if err != nil {
			debug.Printf("ReadFrom failed: %v", err)
			return
		}
		msg, err := parseICMP(v6, fromAddr, rawICMP)
		if err != nil {
			debug.Printf("parseICMP failed: %v", err)
			continue
		}
		ch <- msg
	}
}

func parseICMP(v6 bool, fromAddr net.Addr, rawICMP []byte) (ICMPMessage, error) {
	//debug.Printf("Got message from %v : %v", fromAddr, rawICMP)
	if !v6 {
		switch rawICMP[0] {
		case ICMP4_ECHO_REQUEST:
			debug.Printf("V4Echo Request from %v", fromAddr)
		case ICMP4_ECHO_REPLY:
			debug.Printf("V4Echo Reply from %v", fromAddr)
			id, seq := parseICMPEchoReply(rawICMP)
			return ICMPMessage{int8(rawICMP[0]), int8(rawICMP[1]), fmt.Sprintf("ID %d Sequence %d", id, seq), "odest", "osrc", "oproto"}, nil
		case ICMP4_DEST_UNREACHABLE:
			debug.Printf("V4Dest Unreachable from %v", fromAddr)
			s, d, p := parsev4(rawICMP[8:])
			return ICMPMessage{int8(rawICMP[0]), int8(rawICMP[1]), ICMP4UnreachableCodes[int8(rawICMP[1])], d, s, p}, nil
		case ICMP4_TIME_EXCEEDED:
			debug.Printf("V4Time Exceeded from %v", fromAddr)
			s, d, p := parsev4(rawICMP[8:])
			return ICMPMessage{int8(rawICMP[0]), int8(rawICMP[1]), ICMP4TimeExceededCodes[int8(rawICMP[1])], d, s, p}, nil
		case ICMP4_PARAMETER_PROBLEM:
			debug.Printf("V4Parameter Problem from %v", fromAddr)
			return ICMPMessage{int8(rawICMP[0]), int8(rawICMP[1]), ICMP4ParameterProblemCodes[int8(rawICMP[1])], "odest", "osrc", "oproto"}, nil
		default:
			return ICMPMessage{}, errors.New("Not a useful ICMPv4 message")
		}
	}
	return ICMPMessage{}, errors.New("Unparsable ICMP message")
}

func parsev4(b []byte) (originalLAddr, originalRAddr, originalProto string) {
	hdrlen := (int(b[0]) & 0x0f) << 2
	originalProto = IPProtocol[uint8(b[9])]
	originalLIP := net.IPv4(b[12], b[13], b[14], b[15]).String()
	originalRIP := net.IPv4(b[16], b[17], b[18], b[19]).String()
	originalLPort := fmt.Sprintf("%d", int(b[hdrlen])<<8|int(b[hdrlen+1]))   // true for TCP/UDP/SCTP
	originalRPort := fmt.Sprintf("%d", int(b[hdrlen+2])<<8|int(b[hdrlen+3])) // true for TCP/UDP/SCTP
	originalLAddr = net.JoinHostPort(originalLIP, originalLPort)
	originalRAddr = net.JoinHostPort(originalRIP, originalRPort)
	return
}
