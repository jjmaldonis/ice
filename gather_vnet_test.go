package ice

import (
	"fmt"
	"net"
	"testing"

	"github.com/pion/logging"
	"github.com/pion/transport/vnet"
	"github.com/stretchr/testify/assert"
)

func TestVNetGather(t *testing.T) {
	loggerFactory := logging.NewDefaultLoggerFactory()
	//log := loggerFactory.NewLogger("test")

	t.Run("No local IP address", func(t *testing.T) {
		a, err := NewAgent(&AgentConfig{
			Net: vnet.NewNet(&vnet.NetConfig{}),
		})
		if err != nil {
			t.Fatalf("Failed to create agent: %s", err)
		}

		localIPs, err := a.localInterfaces([]NetworkType{NetworkTypeUDP4})
		if len(localIPs) > 0 {
			t.Fatal("should return no local IP")
		} else if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("Gather a dynamic IP address", func(t *testing.T) {
		cider := "1.2.3.0/24"
		_, ipNet, err := net.ParseCIDR(cider)
		if err != nil {
			t.Fatalf("Failed to parse CIDR: %s", err)
		}

		r, err := vnet.NewRouter(&vnet.RouterConfig{
			CIDR:          cider,
			LoggerFactory: loggerFactory,
		})
		if err != nil {
			t.Fatalf("Failed to create a router: %s", err)
		}

		nw := vnet.NewNet(&vnet.NetConfig{})
		if nw == nil {
			t.Fatalf("Failed to create a Net: %s", err)
		}

		err = r.AddNet(nw)
		if err != nil {
			t.Fatalf("Failed to add a Net to the router: %s", err)
		}

		a, err := NewAgent(&AgentConfig{
			Net: nw,
		})
		if err != nil {
			t.Fatalf("Failed to create agent: %s", err)
		}

		localIPs, err := a.localInterfaces([]NetworkType{NetworkTypeUDP4})
		if len(localIPs) == 0 {
			t.Fatal("should have one local IP")
		} else if err != nil {
			t.Fatal(err)
		}

		for _, ip := range localIPs {
			if ip.IsLoopback() {
				t.Fatal("should not return loopback IP")
			}
			if !ipNet.Contains(ip) {
				t.Fatal("should be contained in the CIDR")
			}
		}
	})

	t.Run("listenUDP", func(t *testing.T) {
		r, err := vnet.NewRouter(&vnet.RouterConfig{
			CIDR:          "1.2.3.0/24",
			LoggerFactory: loggerFactory,
		})
		if err != nil {
			t.Fatalf("Failed to create a router: %s", err)
		}

		nw := vnet.NewNet(&vnet.NetConfig{})
		if nw == nil {
			t.Fatalf("Failed to create a Net: %s", err)
		}

		err = r.AddNet(nw)
		if err != nil {
			t.Fatalf("Failed to add a Net to the router: %s", err)
		}

		a, err := NewAgent(&AgentConfig{Net: nw})
		if err != nil {
			t.Fatalf("Failed to create agent: %s", err)
		}

		localIPs, err := a.localInterfaces([]NetworkType{NetworkTypeUDP4})
		if len(localIPs) == 0 {
			t.Fatal("localInterfaces found no interfaces, unable to test")
		} else if err != nil {
			t.Fatal(err)
		}

		ip := localIPs[0]

		conn, err := a.listenUDP(0, 0, udp, &net.UDPAddr{IP: ip, Port: 0})
		if err != nil {
			t.Fatalf("listenUDP error with no port restriction %v", err)
		} else if conn == nil {
			t.Fatalf("listenUDP error with no port restriction return a nil conn")
		}
		err = conn.Close()
		if err != nil {
			t.Fatalf("failed to close conn")
		}

		_, err = a.listenUDP(4999, 5000, udp, &net.UDPAddr{IP: ip, Port: 0})
		if err != ErrPort {
			t.Fatal("listenUDP with invalid port range did not return ErrPort")
		}

		conn, err = a.listenUDP(5000, 5000, udp, &net.UDPAddr{IP: ip, Port: 0})
		if err != nil {
			t.Fatalf("listenUDP error with no port restriction %v", err)
		} else if conn == nil {
			t.Fatalf("listenUDP error with no port restriction return a nil conn")
		}

		_, port, err := net.SplitHostPort(conn.LocalAddr().String())
		if err != nil {
			t.Fatal(err)
		} else if port != "5000" {
			t.Fatalf("listenUDP with port restriction of 5000 listened on incorrect port (%s)", port)
		}
		err = conn.Close()
		if err != nil {
			t.Fatalf("failed to close conn")
		}
	})
}

func TestVNetGatherWithNAT1To1(t *testing.T) {
	loggerFactory := logging.NewDefaultLoggerFactory()
	log := loggerFactory.NewLogger("test")

	t.Run("gather 1:1 NAT external IPs as host candidates", func(t *testing.T) {
		externalIP0 := "1.2.3.4"
		externalIP1 := "1.2.3.5"
		localIP0 := "10.0.0.1"
		localIP1 := "10.0.0.2"
		map0 := fmt.Sprintf("%s/%s", externalIP0, localIP0)
		map1 := fmt.Sprintf("%s/%s", externalIP1, localIP1)

		wan, err := vnet.NewRouter(&vnet.RouterConfig{
			CIDR:          "1.2.3.0/24",
			LoggerFactory: loggerFactory,
		})
		assert.NoError(t, err, "should succeed")

		lan, err := vnet.NewRouter(&vnet.RouterConfig{
			CIDR:      "10.0.0.0/24",
			StaticIPs: []string{map0, map1},
			NATType: &vnet.NATType{
				Mode: vnet.NATModeNAT1To1,
			},
			LoggerFactory: loggerFactory,
		})
		assert.NoError(t, err, "should succeed")

		err = wan.AddRouter(lan)
		assert.NoError(t, err, "should succeed")

		nw := vnet.NewNet(&vnet.NetConfig{
			StaticIPs: []string{localIP0, localIP1},
		})
		if nw == nil {
			t.Fatalf("Failed to create a Net: %s", err)
		}

		err = lan.AddNet(nw)
		assert.NoError(t, err, "should succeed")

		a, err := NewAgent(&AgentConfig{
			NetworkTypes: []NetworkType{
				NetworkTypeUDP4,
			},
			NAT1To1IPs: []string{map0, map1},
			Trickle:    true,
			Net:        nw,
		})
		assert.NoError(t, err, "should succeed")
		defer a.Close() // nolint:errcheck

		done := make(chan struct{})
		err = a.OnCandidate(func(c Candidate) {
			if c == nil {
				close(done)
			}
		})
		assert.NoError(t, err, "should succeed")

		err = a.GatherCandidates()
		assert.NoError(t, err, "should succeed")

		log.Debug("wait for gathering is done...")
		<-done
		log.Debug("gathering is done")

		candidates, err := a.GetLocalCandidates()
		assert.NoError(t, err, "should succeed")

		if len(candidates) != 2 {
			t.Fatal("There must be two candidates")
		}

		laddr := [2]*net.UDPAddr{nil, nil}
		for i, candi := range candidates {
			laddr[i] = candi.(*CandidateHost).conn.LocalAddr().(*net.UDPAddr)
			if candi.Port() != laddr[i].Port {
				t.Fatalf("Unexpected candidate port: %d", candi.Port())
			}
		}

		if candidates[0].Address() == externalIP0 {
			if candidates[1].Address() != externalIP1 {
				t.Fatalf("Unexpected candidate IP: %s", candidates[1].Address())
			}
			if laddr[0].IP.String() != localIP0 {
				t.Fatalf("Unexpected listen IP: %s", laddr[0].IP.String())
			}
			if laddr[1].IP.String() != localIP1 {
				t.Fatalf("Unexpected listen IP: %s", laddr[1].IP.String())
			}
		} else if candidates[0].Address() == externalIP1 {
			if candidates[1].Address() != externalIP0 {
				t.Fatalf("Unexpected candidate IP: %s", candidates[1].Address())
			}
			if laddr[0].IP.String() != localIP1 {
				t.Fatalf("Unexpected listen IP: %s", laddr[0].IP.String())
			}
			if laddr[1].IP.String() != localIP0 {
				t.Fatalf("Unexpected listen IP: %s", laddr[1].IP.String())
			}
		}
	})

	t.Run("gather 1:1 NAT external IPs as srflx candidates", func(t *testing.T) {
		wan, err := vnet.NewRouter(&vnet.RouterConfig{
			CIDR:          "1.2.3.0/24",
			LoggerFactory: loggerFactory,
		})
		assert.NoError(t, err, "should succeed")

		lan, err := vnet.NewRouter(&vnet.RouterConfig{
			CIDR: "10.0.0.0/24",
			StaticIPs: []string{
				"1.2.3.4/10.0.0.1",
			},
			NATType: &vnet.NATType{
				Mode: vnet.NATModeNAT1To1,
			},
			LoggerFactory: loggerFactory,
		})
		assert.NoError(t, err, "should succeed")

		err = wan.AddRouter(lan)
		assert.NoError(t, err, "should succeed")

		nw := vnet.NewNet(&vnet.NetConfig{
			StaticIPs: []string{
				"10.0.0.1",
			},
		})
		if nw == nil {
			t.Fatalf("Failed to create a Net: %s", err)
		}

		err = lan.AddNet(nw)
		assert.NoError(t, err, "should succeed")

		a, err := NewAgent(&AgentConfig{
			NetworkTypes: []NetworkType{
				NetworkTypeUDP4,
			},
			NAT1To1IPs: []string{
				"1.2.3.4",
			},
			NAT1To1IPCandidateType: CandidateTypeServerReflexive,
			Trickle:                true,
			Net:                    nw,
		})
		assert.NoError(t, err, "should succeed")
		defer a.Close() // nolint:errcheck

		done := make(chan struct{})
		err = a.OnCandidate(func(c Candidate) {
			if c == nil {
				close(done)
			}
		})
		assert.NoError(t, err, "should succeed")

		err = a.GatherCandidates()
		assert.NoError(t, err, "should succeed")

		log.Debug("wait for gathering is done...")
		<-done
		log.Debug("gathering is done")

		candidates, err := a.GetLocalCandidates()
		assert.NoError(t, err, "should succeed")

		if len(candidates) != 2 {
			t.Fatalf("Expected two candidates. actually %d", len(candidates))
		}

		var candiHost *CandidateHost
		var candiSrflx *CandidateServerReflexive

		for _, candidate := range candidates {
			switch candi := candidate.(type) {
			case *CandidateHost:
				candiHost = candi
			case *CandidateServerReflexive:
				candiSrflx = candi
			default:
				t.Fatal("Unexpected candidate type")
			}
		}

		assert.NotNil(t, candiHost, "should not be nil")
		assert.Equal(t, "10.0.0.1", candiHost.Address(), "should match")
		assert.NotNil(t, candiSrflx, "should not be nil")
		assert.Equal(t, "1.2.3.4", candiSrflx.Address(), "should match")
	})
}

func TestVNetGatherWithInterfaceFilter(t *testing.T) {
	loggerFactory := logging.NewDefaultLoggerFactory()
	r, err := vnet.NewRouter(&vnet.RouterConfig{
		CIDR:          "1.2.3.0/24",
		LoggerFactory: loggerFactory,
	})
	if err != nil {
		t.Fatalf("Failed to create a router: %s", err)
	}

	nw := vnet.NewNet(&vnet.NetConfig{})
	if nw == nil {
		t.Fatalf("Failed to create a Net: %s", err)
	}

	if err = r.AddNet(nw); err != nil {
		t.Fatalf("Failed to add a Net to the router: %s", err)
	}

	t.Run("InterfaceFilter should exclude the interface", func(t *testing.T) {
		a, err := NewAgent(&AgentConfig{
			Net: nw,
			InterfaceFilter: func(interfaceName string) bool {
				assert.Equal(t, "eth0", interfaceName)
				return false
			},
		})
		if err != nil {
			t.Fatalf("Failed to create agent: %s", err)
		}

		localIPs, err := a.localInterfaces([]NetworkType{NetworkTypeUDP4})
		if err != nil {
			t.Fatal(err)
		} else if len(localIPs) != 0 {
			t.Fatal("InterfaceFilter should have excluded everything")
		}
	})

	t.Run("InterfaceFilter should not exclude the interface", func(t *testing.T) {
		a, err := NewAgent(&AgentConfig{
			Net: nw,
			InterfaceFilter: func(interfaceName string) bool {
				assert.Equal(t, "eth0", interfaceName)
				return true
			},
		})
		if err != nil {
			t.Fatalf("Failed to create agent: %s", err)
		}

		localIPs, err := a.localInterfaces([]NetworkType{NetworkTypeUDP4})
		if err != nil {
			t.Fatal(err)
		} else if len(localIPs) == 0 {
			t.Fatal("InterfaceFilter should not have excluded anything")
		}
	})
}
