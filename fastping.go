// Package fastping is an ICMP ping library inspired by AnyEvent::FastPing Perl
// module to send ICMP ECHO REQUEST packets quickly. Original Perl module is
// available at
// http://search.cpan.org/~mlehmann/AnyEvent-FastPing-2.01/
//
// It hasn't been fully implemented original functions yet.
//
// Here is an example:
//
//	p := fastping.NewPinger()
//	ra, err := net.ResolveIPAddr("ip4:icmp", os.Args[1])
//	if err != nil {
//		fmt.Println(err)
//		os.Exit(1)
//	}
//	p.AddIPAddr(ra)
//	p.OnRecv = func(addr *net.IPAddr, rtt time.Duration) {
//		fmt.Printf("IP Addr: %s receive, RTT: %v\n", addr.String(), rtt)
//	}
//	p.OnIdle = func() {
//		fmt.Println("finish")
//	}
//	err = p.Run()
//	if err != nil {
//		fmt.Println(err)
//	}
//
// It sends an ICMP packet and wait a response. If it receives a response,
// it calls "receive" callback. After that, MaxRTT time passed, it calls
// "idle" callback. If you need more example, please see "cmd/ping/ping.go".
//
// This library needs to run as a superuser for sending ICMP packets when
// privileged raw ICMP endpoints is used so in such a case, to run go test
// for the package, please run like a following
//
//	sudo go test
//
package fastping

import (
	"errors"
	"fmt"
	"math/rand"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// TimeSliceLength lenght of time slice in bytes
	TimeSliceLength = unsafe.Sizeof(syscall.Timeval{})
	// ProtocolICMP id of ICMP ip proto
	ProtocolICMP = 1
	// ProtocolIPv6ICMP id of ICMPv6 ip proto
	ProtocolIPv6ICMP = 58
)

var (
	ipv4Proto = map[string]string{"ip": "ip4:icmp", "udp": "udp4"}
	ipv6Proto = map[string]string{"ip": "ip6:ipv6-icmp", "udp": "udp6"}
)

func updateBytesTime(buf []byte) {
	syscall.Gettimeofday((*syscall.Timeval)(unsafe.Pointer(&buf[0])))
}

func bytesToTime(b []byte) time.Time {
	sec, nsec := ((*syscall.Timeval)(unsafe.Pointer(&b[0]))).Unix()
	return time.Unix(sec, nsec)
}

func isIPv4(ip net.IP) bool {
	return len(ip.To4()) == net.IPv4len
}

func isIPv6(ip net.IP) bool {
	return len(ip) == net.IPv6len
}

func ipv4Payload(b []byte) []byte {
	if len(b) < ipv4.HeaderLen {
		return b
	}
	hdrlen := int(b[0]&0x0f) << 2
	return b[hdrlen:]
}

type context struct {
	stop chan bool
	done chan bool
	err  error
}

func newContext() *context {
	return &context{
		stop: make(chan bool),
		done: make(chan bool),
	}
}

// Pinger represents ICMP packet sender/receiver
type Pinger struct {
	id  int
	seq int
	// key string is IPAddr.String()
	// addrs map[string]*net.IPAddr
	//	sent    map[string]*net.IPAddr
	index   map[string]int
	paddr   []*net.IPAddr
	pstring []string
	state   []int64
	counter int32
	network string
	source  string
	source6 string
	hasIPv4 bool
	hasIPv6 bool
	ctx     *context
	mu      sync.Mutex
	done    bool

	// Chunk is number of pings to send before sleep
	Chunk int
	// Sleep duration between sending chunks
	Sleep time.Duration

	// Size in bytes of the payload to send
	Size int
	// Number of (nano,milli)seconds of an idle timeout. Once it passed,
	// the library calls an idle callback function. It is also used for an
	// interval time of RunLoop() method
	MaxRTT time.Duration
	// NumGoroutines defines how many goroutines are used when sending ICMP
	// packets and receiving IPv4/IPv6 ICMP responses. Its default is
	// runtime.NumCPU().
	NumGoroutines int
}

// NewPinger returns a new Pinger struct pointer
func NewPinger() *Pinger {
	rand.Seed(time.Now().UnixNano())
	return &Pinger{
		id:            0,
		seq:           0,
		paddr:         []*net.IPAddr{},
		index:         make(map[string]int),
		network:       "ip",
		source:        "",
		source6:       "",
		hasIPv4:       false,
		hasIPv6:       false,
		Size:          int(TimeSliceLength),
		MaxRTT:        time.Second,
		NumGoroutines: runtime.NumCPU(),
	}
}

// Network sets a network endpoints for ICMP ping and returns the previous
// setting. network arg should be "ip" or "udp" string or if others are
// specified, it returns an error. If this function isn't called, Pinger
// uses "ip" as default.
func (p *Pinger) Network(network string) (string, error) {
	origNet := p.network
	switch network {
	case "ip":
		fallthrough
	case "udp":
		p.network = network
	default:
		return origNet, errors.New(network + " can't be used as ICMP endpoint")
	}
	return origNet, nil
}

// Source sets ipv4/ipv6 source IP for sending ICMP packets and returns the previous
// setting. Empty value indicates to use system default one (for both ipv4 and ipv6).
func (p *Pinger) Source(source string) (string, error) {
	if source == p.source {
		return p.source, nil
	}

	// using ipv4 previous value for new empty one
	origSource := p.source
	if "" == source {
		p.source = ""
		p.source6 = ""
		return origSource, nil
	}

	addr := net.ParseIP(source)
	if addr == nil {
		return origSource, errors.New(source + " is not a valid textual representation of an IPv4/IPv6 address")
	}

	if isIPv4(addr) {
		p.source = source
	} else if isIPv6(addr) {
		origSource = p.source6
		p.source6 = source
	} else {
		return origSource, errors.New(source + " is not a valid textual representation of an IPv4/IPv6 address")
	}

	return origSource, nil
}

// AddIP adds an IP address to Pinger. ipaddr arg should be a string like
// "192.0.2.1".
func (p *Pinger) AddIP(ipaddr string) error {
	addr := net.ParseIP(ipaddr)
	if addr == nil {
		return fmt.Errorf("%s is not a valid textual representation of an IP address", ipaddr)
	}
	addrStr := addr.String()

	if _, ok := p.index[addrStr]; ok {
		return nil
	}

	p.index[addrStr] = len(p.paddr)
	p.paddr = append(p.paddr, &net.IPAddr{IP: addr})
	p.pstring = append(p.pstring, addrStr)

	if isIPv4(addr) {
		p.hasIPv4 = true
	} else if isIPv6(addr) {
		p.hasIPv6 = true
	}
	return nil
}

// AddIPAddr adds an IP address to Pinger. ip arg should be a net.IPAddr
// pointer.
func (p *Pinger) AddIPAddr(ip *net.IPAddr) {
	addrStr := ip.String()

	if _, ok := p.index[addrStr]; ok {
		return
	}

	p.index[addrStr] = len(p.paddr)
	p.paddr = append(p.paddr, ip)
	p.pstring = append(p.pstring, addrStr)

	if isIPv4(ip.IP) {
		p.hasIPv4 = true
	} else if isIPv6(ip.IP) {
		p.hasIPv6 = true
	}
}

// RemoveIP removes an IP address from Pinger. ipaddr arg should be a string
// like "192.0.2.1".
func (p *Pinger) RemoveIP(ipaddr string) error {
	addr := net.ParseIP(ipaddr)
	if addr == nil {
		return fmt.Errorf("%s is not a valid textual representation of an IP address", ipaddr)
	}

	index := p.index[ipaddr]

	p.paddr = append(p.paddr[:index], p.paddr[index+1:]...)
	p.pstring = append(p.pstring[:index], p.pstring[index+1:]...)

	delete(p.index, addr.String())

	p.updateIndexes()

	return nil
}

// Run invokes a single send/receive procedure. It sends packets to all hosts
// which have already been added by AddIP() etc. and wait those responses. When
// it receives a response, it calls "receive" handler registered by AddHander().
// After MaxRTT seconds, it calls "idle" handler and returns to caller with
// an error value. It means it blocks until MaxRTT seconds passed.
func (p *Pinger) Run(skip map[string]bool, id int, seq int) (map[string]time.Duration, error) {
	p.ctx = newContext()
	p.id = id
	p.seq = seq
	p.run(skip)

	result := make(map[string]time.Duration, len(p.index))
	for ip, index := range p.index {
		result[ip] = time.Duration(p.state[index])
	}

	return result, p.ctx.err
}

func (p *Pinger) listen(netProto string, source string) *icmp.PacketConn {
	conn, err := icmp.ListenPacket(netProto, source)
	if err != nil {
		p.ctx.err = err
		p.ctx.done <- true
		return nil
	}
	return conn
}

func (p *Pinger) run(skip map[string]bool) {
	p.state = make([]int64, len(p.index))
	p.counter = int32(len(p.index))
	p.done = false
	if 0 == p.id && 0 == p.seq {
		p.id = rand.Intn(0xffff)
		p.seq = rand.Intn(0xffff)
	}

	var conn, conn6 *icmp.PacketConn
	if p.hasIPv4 {
		if conn = p.listen(ipv4Proto[p.network], p.source); conn == nil {
			return
		}
		defer conn.Close()
	}

	if p.hasIPv6 {
		if conn6 = p.listen(ipv6Proto[p.network], p.source6); conn6 == nil {
			return
		}
		defer conn6.Close()
	}

	recvCtx := newContext()
	wg := new(sync.WaitGroup)

	if conn != nil {
		routines := p.NumGoroutines
		wg.Add(routines)
		for i := 0; i < routines; i++ {
			go p.recvICMP(conn, recvCtx, wg)
		}
	}

	if conn6 != nil {
		routines := p.NumGoroutines
		wg.Add(routines)
		for i := 0; i < routines; i++ {
			go p.recvICMP(conn6, recvCtx, wg)
		}
	}

	p.sendICMP(conn, conn6, skip)

	select {
	case <-recvCtx.done:
	case <-time.After(p.MaxRTT):
	}

	close(recvCtx.stop)
	wg.Wait()

	p.mu.Lock()
	p.ctx.err = recvCtx.err

	if !p.done {
		p.done = true
		close(p.ctx.done)
	}
	p.mu.Unlock()
}

func (p *Pinger) sendICMP(conn, conn6 *icmp.PacketConn, skip map[string]bool) {

	wg := new(sync.WaitGroup)
	buf := make([]byte, p.Size)

	// prefill payload as usual ping command do
	for i := uint16(TimeSliceLength); i < uint16(p.Size); i++ {
		buf[i] = byte(i & 0xff)
	}

	sendPacket := func(from, to, chunk int) {
		defer wg.Done()

		for i := from; i < to; i++ {
			if (i > from) && (chunk > 0) && (((i - from) % chunk) == 0) {
				time.Sleep(p.Sleep)
			}

			if skip[p.pstring[i]] {
				continue
			}

			addr := p.paddr[i]

			var typ icmp.Type
			var cn *icmp.PacketConn
			if isIPv4(addr.IP) {
				typ = ipv4.ICMPTypeEcho
				cn = conn
			} else if isIPv6(addr.IP) {
				typ = ipv6.ICMPTypeEchoRequest
				cn = conn6
			} else {
				continue
			}
			if cn == nil {
				continue
			}

			updateBytesTime(buf)

			bytes, err := (&icmp.Message{
				Type: typ, Code: 0,
				Body: &icmp.Echo{
					ID: p.id, Seq: p.seq,
					Data: buf,
				},
			}).Marshal(nil)

			// this is almost impossible
			if err != nil {
				continue
			}

			var dst net.Addr = addr
			if p.network == "udp" {
				dst = &net.UDPAddr{IP: addr.IP, Zone: addr.Zone}
			}

			// pre-add ip to sent
			p.state[i] = -1

			for {
				if _, err := cn.WriteTo(bytes, dst); err != nil {
					if neterr, ok := err.(*net.OpError); ok {
						if neterr.Err == syscall.ENOBUFS {
							continue
						}
					}
					p.state[i] = -2
					atomic.AddInt32(&p.counter, -1)
				}
				break
			}
		}
	}

	total := len(p.index)

	step := total/p.NumGoroutines + 1
	chunk := p.Chunk / p.NumGoroutines
	if chunk < 5 {
		chunk = 0
	}

	for i := 0; i < total; i += step {
		wg.Add(1)
		to := i + step
		if to > total {
			to = total
		}
		go sendPacket(i, to, chunk)
	}

	wg.Wait()

}

func (p *Pinger) recvICMP(conn *icmp.PacketConn, ctx *context, wg *sync.WaitGroup) {

	defer wg.Done()

	bytes := make([]byte, 512)

	for {
		select {
		case <-ctx.stop:
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(time.Millisecond * 50))

		_, ra, err := conn.ReadFrom(bytes)

		if err != nil {
			if neterr, ok := err.(*net.OpError); ok {
				if neterr.Timeout() {
					continue
				} else {
					// prevent 2x close in different threads
					p.mu.Lock()
					if ctx.err == nil && !p.done {
						p.done = true
						close(ctx.done)
					}
					ctx.err = err
					p.mu.Unlock()
					return
				}
			}
		}
		p.procRecv(bytes, ra, ctx)
	}
}

func (p *Pinger) procRecv(bytes []byte, ra net.Addr, ctx *context) {
	var ipaddr *net.IPAddr
	switch adr := ra.(type) {
	case *net.IPAddr:
		ipaddr = adr
	case *net.UDPAddr:
		ipaddr = &net.IPAddr{IP: adr.IP, Zone: adr.Zone}
	default:
		return
	}

	addr := ipaddr.String()
	_, ok := p.index[addr]

	if !ok {
		return
	}

	var proto int
	if isIPv4(ipaddr.IP) {
		if p.network == "ip" {
			bytes = ipv4Payload(bytes)
		}
		proto = ProtocolICMP
	} else if isIPv6(ipaddr.IP) {
		proto = ProtocolIPv6ICMP
	} else {
		return
	}

	var m *icmp.Message
	var err error
	if m, err = icmp.ParseMessage(proto, bytes); err != nil {
		return
	}

	if m.Type != ipv4.ICMPTypeEchoReply && m.Type != ipv6.ICMPTypeEchoReply {
		return
	}

	var rtt time.Duration
	switch pkt := m.Body.(type) {
	case *icmp.Echo:
		if pkt.ID == p.id && pkt.Seq == p.seq && len(pkt.Data) >= int(TimeSliceLength) {
			rtt = time.Since(bytesToTime(pkt.Data))
		}
	default:
		return
	}

	if 0 == rtt {
		return
	}

	current := atomic.AddInt32(&p.counter, -1)
	p.state[p.index[addr]] = int64(rtt)

	if 0 == current {
		p.mu.Lock()
		if !p.done {
			p.done = true
			close(ctx.done)
		}

		p.mu.Unlock()
	}
}

func (p *Pinger) updateIndexes() {
	for index, ip := range p.pstring {
		p.index[ip] = index
	}
}
