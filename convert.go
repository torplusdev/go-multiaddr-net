package manet

import (
	"encoding/base32"
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	ma "github.com/multiformats/go-multiaddr"
)

var errIncorrectNetAddr = fmt.Errorf("incorrect network addr conversion")

// FromNetAddr converts a net.Addr type to a Multiaddr.
func FromNetAddr(a net.Addr) (ma.Multiaddr, error) {
	return defaultCodecs.FromNetAddr(a)
}

// FromNetAddr converts a net.Addr to Multiaddress.
func (cm *CodecMap) FromNetAddr(a net.Addr) (ma.Multiaddr, error) {
	if a == nil {
		return nil, fmt.Errorf("nil multiaddr")
	}

	//TODO:Refactor to use parsers
	if a.Network() == "onion3" {

		fields := strings.Split(a.String(), ":")
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid onion addr doesn't contain a port %v", a)
		}

		// Get Tor Onion Addr
		onionm, err := ma.NewMultiaddr(fmt.Sprintf("/onion3/%s:%s", fields[0], fields[1]))
		if err != nil {
			return nil, errIncorrectNetAddr
		}
		return onionm, nil
	}

	p, err := cm.getAddrParser(a.Network())
	if err != nil {
		return nil, err
	}

	return p(a)
}

// ToNetAddr converts a Multiaddr to a net.Addr
// Must be ThinWaist. acceptable protocol stacks are:
// /ip{4,6}/{tcp, udp}
func ToNetAddr(maddr ma.Multiaddr) (net.Addr, error) {
	return defaultCodecs.ToNetAddr(maddr)
}

// ToNetAddr converts a Multiaddress to a standard net.Addr.
func (cm *CodecMap) ToNetAddr(maddr ma.Multiaddr) (net.Addr, error) {
	protos := maddr.Protocols()
	final := protos[len(protos)-1]

	p, err := cm.getMaddrParser(final.Name)
	if err != nil {
		return nil, err
	}

	return p(maddr)
}

func parseBasicNetMaddr(maddr ma.Multiaddr) (net.Addr, error) {
	network, host, err := DialArgs(maddr)
	if err != nil {
		return nil, err
	}

	switch network {
	case "tcp", "tcp4", "tcp6":
		return net.ResolveTCPAddr(network, host)
	case "udp", "udp4", "udp6":
		return net.ResolveUDPAddr(network, host)
	case "ip", "ip4", "ip6":
		return net.ResolveIPAddr(network, host)

	case "unix":
		return net.ResolveUnixAddr(network, host)
	}

	return nil, fmt.Errorf("network not supported: %s", network)
}

func FromIPAndZone(ip net.IP, zone string) (ma.Multiaddr, error) {
	switch {
	case ip.To4() != nil:
		return ma.NewComponent("ip4", ip.String())
	case ip.To16() != nil:
		ip6, err := ma.NewComponent("ip6", ip.String())
		if err != nil {
			return nil, err
		}
		if zone == "" {
			return ip6, nil
		} else {
			zone, err := ma.NewComponent("ip6zone", zone)
			if err != nil {
				return nil, err
			}
			return zone.Encapsulate(ip6), nil
		}
	default:
		return nil, errIncorrectNetAddr
	}
}

// FromIP converts a net.IP type to a Multiaddr.
func FromIP(ip net.IP) (ma.Multiaddr, error) {
	return FromIPAndZone(ip, "")
}

// DialArgs is a convenience function that returns network and address as
// expected by net.Dial. See https://godoc.org/net#Dial for an overview of
// possible return values (we do not support the unixpacket ones yet). Unix
// addresses do not, at present, compose.
func DialArgs(m ma.Multiaddr) (string, string, error) {
	var (
		zone, network, ip, port string
		err                     error
		hostname                bool
	)

	ma.ForEach(m, func(c ma.Component) bool {
		switch network {
		case "onion", "onion3":
			return true

		case "":
			switch c.Protocol().Code {
			case ma.P_IP6ZONE:
				if zone != "" {
					err = fmt.Errorf("%s has multiple zones", m)
					return false
				}
				zone = c.Value()
				return true
			case ma.P_IP6:
				network = "ip6"
				ip = c.Value()
				return true
			case ma.P_IP4:
				if zone != "" {
					err = fmt.Errorf("%s has ip4 with zone", m)
					return false
				}
				network = "ip4"
				ip = c.Value()
				return true
			case ma.P_DNS4:
				network = "ip4"
				hostname = true
				ip = c.Value()
				return true
			case ma.P_DNS6:
				network = "ip6"
				hostname = true
				ip = c.Value()
				return true
			case ma.P_UNIX:
				network = "unix"
				ip = c.Value()
				return false
			}
		case "ip4":
			switch c.Protocol().Code {
			case ma.P_UDP:
				network = "udp4"
			case ma.P_TCP:
				network = "tcp4"
			default:
				return false
			}
			port = c.Value()
		case "ip6":
			switch c.Protocol().Code {
			case ma.P_UDP:
				network = "udp6"
			case ma.P_TCP:
				network = "tcp6"
			default:
				return false
			}
			port = c.Value()
		}
		// Done.
		return false
	})
	if err != nil {
		return "", "", err
	}

	switch network {
	case "ip6":
		if zone != "" {
			ip += "%" + zone
		}
		fallthrough
	case "ip4":
		return network, ip, nil
	case "tcp4", "udp4":
		return network, ip + ":" + port, nil
	case "tcp6", "udp6":
		if zone != "" {
			ip += "%" + zone
		}
		if hostname {
			return network, ip + ":" + port, nil
		}
		return network, "[" + ip + "]" + ":" + port, nil
	case "unix":
		if runtime.GOOS == "windows" {
			// convert /c:/... to c:\...
			ip = filepath.FromSlash(strings.TrimLeft(ip, "/"))
		}
		return network, ip, nil
	default:
		return "", "", fmt.Errorf("%s is not a 'thin waist' address", m)
	}
}

func parseOnionNetAddr(a net.Addr) (ma.Multiaddr, error) {

	m,err := ma.NewMultiaddr(a.String())
	var addressLength int = 16;

	if len(m.Protocols()) != 1 {
		return nil, fmt.Errorf("incorrect protocol")
	}

	var addr string;

	switch (m.Protocols()[0].Name) {
	case "onion":
		fallthrough;
	case "onion3":
		addressLength = 56;
		addr, err = m.ValueForProtocol(ma.P_ONION3)
		if err != nil {
			return nil, fmt.Errorf("couldnt split into address and port")
		}
		break;
	default:
		return nil, fmt.Errorf("incorrect prefix")

	}
	// check for correct network type
	//if m.Protocols()[0].Name != "onion3" {
	//	return nil, fmt.Errorf("incorrect prefix")
	//}

	// split into onion address and port
	//addr, err := m.ValueForProtocol(ma.P_ONION3)
	//if err != nil {
	//	return nil, fmt.Errorf("couldnt split into address and port")
	//}
	split := strings.Split(addr, ":")
	if len(split) != 2 {
		return nil, fmt.Errorf("too many elements")
	}

	// onion3 address without the ".onion" substring
	if len(split[0]) != addressLength {
		fmt.Println(split[0])
		return nil, fmt.Errorf("bad address length")
	}
	_, err = base32.StdEncoding.DecodeString(strings.ToUpper(split[0]))
	if err != nil {
		return nil, fmt.Errorf("couldnt decode address")
	}

	// onion port number
	i, err := strconv.Atoi(split[1])
	if err != nil {
		return nil, fmt.Errorf("couldnt decode port")
	}
	if i >= 65536 || i < 1 {
		return nil, fmt.Errorf("invalid port number")
	}

	return m,err
}

func parseTCPNetAddr(a net.Addr) (ma.Multiaddr, error) {
	ac, ok := a.(*net.TCPAddr)
	if !ok {
		return nil, errIncorrectNetAddr
	}

	// Get IP Addr
	ipm, err := FromIPAndZone(ac.IP, ac.Zone)
	if err != nil {
		return nil, errIncorrectNetAddr
	}

	// Get TCP Addr
	tcpm, err := ma.NewMultiaddr(fmt.Sprintf("/tcp/%d", ac.Port))
	if err != nil {
		return nil, errIncorrectNetAddr
	}

	// Encapsulate
	return ipm.Encapsulate(tcpm), nil
}

func parseUDPNetAddr(a net.Addr) (ma.Multiaddr, error) {
	ac, ok := a.(*net.UDPAddr)
	if !ok {
		return nil, errIncorrectNetAddr
	}

	// Get IP Addr
	ipm, err := FromIPAndZone(ac.IP, ac.Zone)
	if err != nil {
		return nil, errIncorrectNetAddr
	}

	// Get UDP Addr
	udpm, err := ma.NewMultiaddr(fmt.Sprintf("/udp/%d", ac.Port))
	if err != nil {
		return nil, errIncorrectNetAddr
	}

	// Encapsulate
	return ipm.Encapsulate(udpm), nil
}

func parseIPNetAddr(a net.Addr) (ma.Multiaddr, error) {
	ac, ok := a.(*net.IPAddr)
	if !ok {
		return nil, errIncorrectNetAddr
	}
	return FromIPAndZone(ac.IP, ac.Zone)
}

func parseIPPlusNetAddr(a net.Addr) (ma.Multiaddr, error) {
	ac, ok := a.(*net.IPNet)
	if !ok {
		return nil, errIncorrectNetAddr
	}
	return FromIP(ac.IP)
}

func parseUnixNetAddr(a net.Addr) (ma.Multiaddr, error) {
	ac, ok := a.(*net.UnixAddr)
	if !ok {
		return nil, errIncorrectNetAddr
	}

	path := ac.Name
	if runtime.GOOS == "windows" {
		// Convert c:\foobar\... to c:/foobar/...
		path = filepath.ToSlash(path)
	}
	if len(path) == 0 || path[0] != '/' {
		// convert "" and "c:/..." to "/..."
		path = "/" + path
	}

	return ma.NewComponent("unix", path)
}
