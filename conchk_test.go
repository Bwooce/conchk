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
       "testing"
)

var defaultTests = []Test{
	{"ICMPv4 localhost", "ip4:icmp", "", "127.0.0.1", false, "", "", false, false, ""},
	{"ICMPv6 localhost", "ip6:icmp", "", "::1", true, "", "", false, false, ""},
	{"UDP localhost:80", "udp4", "localhost:1025", "127.0.0.1:80", false, "", "", false, false, ""},
	{"TCP localhost:http", "tcp4", "", "127.0.0.1:80", false, "", "", false, false, ""},
	{"TCP bad.example.com:http", "tcp4", "", "bad.example.com:http", false, "", "", false, false, ""},
}

func TestIsV6(t *testing.T) {
	var v4 = []string { "1.1.1.1", "10.10.10.10", "255.255.255.255", "1.0.255.254", "0.0.0.0", "1.1.1.1:1", "23.12.167.1:65535" }
	var v6 = []string { "[::1]", "[FE80:0000:0000:0000:0202:B3FF: FE1E:8329]", "[fdf8:f53b:82e4::53]", "[::ffff:192.0.2.47]", "[2001:db8:8:4::2]" }
	
	for _, ip := range v4 {
		result := isV6(ip)
		if(result) {
			t.Fatal("Address is v4 but thinks is v6:", ip)
		}
	}
	for _, ip := range v6 {
		result := isV6(ip)
		if(!result) {
			t.Fatal("Address is v6 but thinks is v4:", ip)
		}
	}

}