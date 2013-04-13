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
	"time"
	"log"
)

type ICMPMessage struct {

}

type ICMPPublisher struct {
	subs	map[chan ICMPMessage]bool
	sub	    chan chan ICMPMessage
	unsub	chan chan ICMPMessage
}

const ChanDepth = 10

const (
    ICMP4_ECHO_REQUEST = 8
    ICMP4_ECHO_REPLY   = 0
	ICMP4_DEST_UNREACHABLE = 3
	ICMP4_TIME_EXCEEDED = 11
	ICMP4_PARAMETER_PROBLEM = 12

	ICMP6_DEST_UNREACHABLE = 1
	ICMP6_PACKET_TOO_BIG = 2
	ICMP6_TIME_EXCEEDED = 3
	ICMP6_PARAMETER_PROBLEM = 4
    ICMP6_ECHO_REQUEST = 128
    ICMP6_ECHO_REPLY   = 129
)

// ICMP4_DEST_UNREACHABLE codes
var ICMP4UnreachableCodes = []struct {
	code int
	desc string
} {
	{0,"Network unreachable error"},
	{1,"Host unreachable error"},
	{2,"Protocol unreachable error (the designated transport protocol is not supported)"},
	{3,"Port unreachable error (the designated protocol is unable to inform the host of the incoming message)"},
	{4,"The datagram is too big. Packet fragmentation is required but the 'don't fragment' (DF) flag is on."},
	{5,"Source route failed error"},
	{6,"Destination network unknown error"},
	{7,"Destination host unknown error"},
	{8,"Source host isolated error"},
	{9,"The destination network is administratively prohibited"},
	{10,"The destination host is administratively prohibited"},
	{11,"The network is unreachable for Type Of Service"},
	{12,"The host is unreachable for Type Of Service"},
	{13,"Communication administratively prohibited (administrative filtering prevents packet from being forwarded)"},
	{14,"Host precedence violation (indicates the requested precedence is not permitted for the combination of host or network and port)"},
	{15,"Precedence cutoff in effect (precedence of datagram is below the level set by the network administrators)"},
}

func NewICMPPublisher() (*ICMPPublisher,chan ICMPMessage) {
	p := &ICMPPublisher{
		subs:	make(map[chan ICMPMessage]bool),
		sub:	make(chan chan ICMPMessage),
		unsub:	make(chan chan ICMPMessage),
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


func waitForPossibleICMP(test *Test, icmpCh chan ICMPMessage) {
	timeout := make(chan bool, 1)
    go func() {
        time.Sleep(1 * time.Second)
        timeout <- true
    }()
	
	select {
	case msg := <- icmpCh:
		match, err := matchICMP(test, msg)
		if match {
			test.run = true
			test.passed = false
			log.Println(fmtTest(*test), "ICMP", err)
		}
	case <- timeout: // this is the normal case
	}
}

func matchICMP(test *Test, msg ICMPMessage) (bool, error) {
	return false, nil
}

func parseICMPEchoReply(b []byte) (id, seqnum int) {
   	id = int(b[4])<<8 | int(b[5])
   	seqnum = int(b[6])<<8 | int(b[7])
  	return
}
