go-fastping
===========

WARNING: this is thread-unsafe fork of original https://github.com/tatsushid/go-fastping with
many changes (limit usage of locks, channels, send packets in chunks and many
other).

In common cases please use original version.

---

go-fastping is a Go language's ICMP ping library inspired by AnyEvent::FastPing
Perl module to send ICMP ECHO REQUEST packets quickly. Original Perl module is
available at
http://search.cpan.org/~mlehmann/AnyEvent-FastPing-2.01/

It hasn't been fully implemented original functions yet.

## Installation

Install and update this go package with `go get -u github.com/kanocz/go-fastping`

## Examples

Import this package and write

```go
p := fastping.NewPinger()
p.AddIP(os.Args[1])

result, err := p.Run(map[string]bool{},0,0)
if err != nil {
	fmt.Println(err)
} else {
	fmt.Printf("Result: %s:%+v\n",os.Args[1], result[os.Args[1]])
}
```

It sends an ICMP packet and wait a response. Result is map with ip->rtt, in case
of loss rtt == -1

## Caution
This package implements ICMP ping using both raw socket and UDP. If your program
uses this package in raw socket mode, it needs to be run as a root user.

## License
go-fastping is under MIT License. See the [LICENSE][license] file for details.
