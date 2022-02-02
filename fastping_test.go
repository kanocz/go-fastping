package fastping

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSource(t *testing.T) {
	for i, tt := range []struct {
		firstAddr  string
		secondAddr string
		invalid    bool
	}{
		{firstAddr: "192.0.2.10", secondAddr: "192.0.2.20", invalid: false},
		{firstAddr: "2001:0DB8::10", secondAddr: "2001:0DB8::20", invalid: false},
		{firstAddr: "192.0.2", invalid: true},
	} {
		p := NewPinger()

		origSource, err := p.Source(tt.firstAddr)
		if tt.invalid {
			if err == nil {
				t.Errorf("[%d] Source should return an error but nothing: %v", i, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("[%d] Source address failed: %v", i, err)
		}
		if origSource != "" {
			t.Errorf("[%d] Source returned an unexpected value: got %q, expected %q", i, origSource, "")
		}

		origSource, err = p.Source(tt.secondAddr)
		if err != nil {
			t.Errorf("[%d] Source address failed: %v", i, err)
		}
		if origSource != tt.firstAddr {
			t.Errorf("[%d] Source returned an unexpected value: got %q, expected %q", i, origSource, tt.firstAddr)
		}
	}

	v4Addr := "192.0.2.10"
	v6Addr := "2001:0DB8::10"

	p := NewPinger()
	_, err := p.Source(v4Addr)
	if err != nil {
		t.Errorf("Source address failed: %v", err)
	}
	_, err = p.Source(v6Addr)
	if err != nil {
		t.Errorf("Source address failed: %v", err)
	}
	origSource, err := p.Source("")
	if err != nil {
		t.Errorf("Source address failed: %v", err)
	}
	if origSource != v4Addr {
		t.Errorf("Source returned an unexpected value: got %q, expected %q", origSource, v4Addr)
	}
}

func TestAddIP(t *testing.T) {
	addIPTests := []struct {
		host   string
		addr   *net.IPAddr
		expect bool
	}{
		{host: "127.0.0.1", addr: &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)}, expect: true},
		{host: "localhost", addr: &net.IPAddr{IP: net.IPv4(127, 0, 0, 1)}, expect: false},
	}

	p := NewPinger()

	for _, tt := range addIPTests {
		if ok := p.AddIP(tt.host); ok != nil {
			if tt.expect != false {
				t.Errorf("AddIP failed: got %v, expected %v", ok, tt.expect)
			}
		}
	}
	for _, tt := range addIPTests {
		if tt.expect {
			if !p.paddr[p.index[tt.host]].IP.Equal(tt.addr.IP) {
				t.Errorf("AddIP didn't save IPAddr: %v", tt.host)
			}
		}
	}
}

func TestAddIPAddr(t *testing.T) {
	addIPAddrTests := []*net.IPAddr{
		{IP: net.IPv4(192, 0, 2, 10)},
		{IP: net.IP{0x20, 0x01, 0x0D, 0xB8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10}},
	}

	p := NewPinger()

	for i, tt := range addIPAddrTests {
		p.AddIPAddr(tt)
		if !p.paddr[p.index[tt.String()]].IP.Equal(tt.IP) {
			t.Errorf("[%d] AddIPAddr didn't save IPAddr: %v", i, tt.IP)
		}
		if len(tt.IP.To4()) == net.IPv4len {
			if p.hasIPv4 != true {
				t.Errorf("[%d] AddIPAddr didn't save IPAddr type: got %v, expected %v", i, p.hasIPv4, true)
			}
		} else if len(tt.IP) == net.IPv6len {
			if p.hasIPv6 != true {
				t.Errorf("[%d] AddIPAddr didn't save IPAddr type: got %v, expected %v", i, p.hasIPv6, true)
			}
		} else {
			t.Errorf("[%d] AddIPAddr encounted an unexpected error", i)
		}
	}
}

func TestRun(t *testing.T) {
	for _, network := range []string{"ip", "udp"} {
		p := NewPinger()
		p.Network(network)

		if err := p.AddIP("127.0.0.1"); err != nil {
			t.Fatalf("AddIP failed: %v", err)
		}

		if err := p.AddIP("127.0.0.100"); err != nil {
			t.Fatalf("AddIP failed: %v", err)
		}

		if err := p.AddIP("::1"); err != nil {
			t.Fatalf("AddIP failed: %v", err)
		}

		result, err := p.Run(map[string]bool{})

		found1, found100, foundv6 := false, false, false

		_, found1 = result["127.0.0.1"]
		_, found100 = result["127.0.0.100"]
		_, foundv6 = result["::1"]

		if err != nil {
			t.Fatalf("Pinger returns error: %v", err)
		}
		if !found1 {
			t.Fatalf("Pinger `127.0.0.1` didn't respond")
		}
		if found100 {
			t.Fatalf("Pinger `127.0.0.100` responded")
		}
		if !foundv6 {
			t.Fatalf("Pinger `::1` didn't responded")
		}
	}
}

func TestListen(t *testing.T) {
	noSource := ""
	invalidSource := "192.0.2"

	p := NewPinger()
	p.ctx = newContext()

	conn := p.listen("ip4:icmp", noSource)
	if conn == nil {
		t.Errorf("listen failed: %v", p.ctx.err)
	} else {
		conn.Close()
	}

	conn = p.listen("ip4:icmp", invalidSource)
	if conn != nil {
		t.Errorf("listen should return nothing but something: %v", conn)
		conn.Close()
	}
}

// func TestTimeToBytes(t *testing.T) {
// 	// 2009-11-10 23:00:00 +0000 UTC = 1257894000000000000
// 	expect := []byte{0x00, 0x60, 0x18, 0xab, 0xed, 0xef, 0x74, 0x11}
// 	tm, err := time.Parse(time.RFC3339, "2009-11-10T23:00:00Z")
// 	if err != nil {
// 		t.Errorf("time.Parse failed: %v", err)
// 	}
// 	b := make([]byte, 8)
// 	updateBytesTime(b)
// 	for i := 0; i < 8; i++ {
// 		if b[i] != expect[i] {
// 			t.Errorf("timeToBytes failed: got %v, expected: %v", b, expect)
// 			break
// 		}
// 	}
// }

func TestBytesToTime(t *testing.T) {
	// 2009-11-10 23:00:00 +0000 UTC = 1257894000000000000
	b := []byte{0x00, 0x60, 0x18, 0xab, 0xed, 0xef, 0x74, 0x11}
	expect, err := time.Parse(time.RFC3339, "2009-11-10T23:00:00Z")
	if err != nil {
		t.Errorf("time.Parse failed: %v", err)
	}
	tm := bytesToTime(b)
	if !tm.Equal(expect) {
		t.Errorf("bytesToTime failed: got %v, expected: %v", tm.UTC(), expect.UTC())
	}
}

func TestTimeToBytesToTime(t *testing.T) {
	tm, err := time.Parse(time.RFC3339, "2009-11-10T23:00:00Z")
	if err != nil {
		t.Errorf("time.Parse failed: %v", err)
	}
	b := make([]byte, 8)
	updateBytesTime(b)
	tm2 := bytesToTime(b)
	if !tm.Equal(tm2) {
		t.Errorf("bytesToTime failed: got %v, expected: %v", tm2.UTC(), tm.UTC())
	}
}

func TestPayloadSizeDefault(t *testing.T) {
	s := make([]byte, 8)
	updateBytesTime(s)

	d := append(s, make([]byte, 8-TimeSliceLength)...)

	if len(d) != 8 {
		t.Errorf("Payload size incorrect: got %d, expected: %d", len(d), 8)
	}
}

func TestPayloadSizeCustom(t *testing.T) {
	s := make([]byte, 8)
	updateBytesTime(s)
	d := append(s, make([]byte, 64-TimeSliceLength)...)

	if len(d) != 64 {
		t.Errorf("Payload size incorrect: got %d, expected: %d", len(d), 64)
	}
}

func TestRemoveIp(t *testing.T) {
	p := NewPinger()

	assert.NoError(t, p.AddIP("1.1.1.1"))
	assert.NoError(t, p.AddIP("2.2.2.2"))
	assert.NoError(t, p.AddIP("3.3.3.3"))
	assert.NoError(t, p.AddIP("4.4.4.4"))

	indexes := map[string]int{
		"1.1.1.1": 0,
		"2.2.2.2": 1,
		"3.3.3.3": 2,
		"4.4.4.4": 3,
	}

	results, err := p.Run(map[string]bool{}, 1, 1)

	assert.NoError(t, err)
	assert.Equal(t, len(results), 4)
	assert.Equal(t, p.index, indexes)

	assert.NoError(t, p.RemoveIP("3.3.3.3"))
	delete(indexes, "3.3.3.3")
	indexes["4.4.4.4"] = 2

	_, err = p.Run(map[string]bool{}, 1, 1)
	assert.NoError(t, err)

	assert.Equal(t, p.index, indexes)
}
