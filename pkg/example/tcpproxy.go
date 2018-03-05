package main

import (
	"time"
	"net"
	"bytes"
	"fmt"
	"gitlab.alipay-inc.com/afe/mosn/pkg/server"
	"gitlab.alipay-inc.com/afe/mosn/pkg/api/v2"
	"gitlab.alipay-inc.com/afe/mosn/pkg/network"
	"gitlab.alipay-inc.com/afe/mosn/pkg/types"
	"gitlab.alipay-inc.com/afe/mosn/pkg/server/config/proxy"
)

const (
	TestCluster    = "tstCluster"
	TestListener   = "tstListener"
	RealServerAddr = "127.0.0.1:8080"
	MeshServerAddr = "127.0.0.1:2048"
)

func main() {
	stopChan := make(chan bool)
	upstreamReadyChan := make(chan bool)
	meshReadyChan := make(chan bool)

	go func() {
		// upstream
		l, _ := net.Listen("tcp", RealServerAddr)
		fmt.Println("listen on ")
		defer l.Close()

		for {
			select {
			case <-stopChan:
				break
			default:
				upstreamReadyChan <- true

				conn, _ := l.Accept()

				fmt.Printf("[REALSERVER]get connection %s..", conn.RemoteAddr())
				fmt.Println()

				buf := make([]byte, 4*1024)

				for {
					t := time.Now()
					conn.SetReadDeadline(t.Add(3 * time.Second))

					if bytesRead, err := conn.Read(buf); err != nil {

						if err, ok := err.(net.Error); ok && err.Timeout() {
							continue
						}

						fmt.Println("[REALSERVER]failed read buf")
						return
					} else {
						if bytesRead > 0 {
							fmt.Printf("[REALSERVER]get data '%s'", string(buf[:bytesRead]))
							fmt.Println()
							break
						}
					}
				}

				fmt.Printf("[REALSERVER]write back data 'world'")
				fmt.Println()

				conn.Write([]byte("world"))

				select {
				case <-stopChan:
					conn.Close()
				}
			}
		}
	}()

	go func() {
		select {
		case <-upstreamReadyChan:
			// mesh
			cmf := &clusterManagerFilter{}
			srv := server.NewServer(&proxy.TcpProxyFilterConfigFactory{
				Proxy: tcpProxyConfig(),
			}, cmf)
			srv.AddListener(tcpProxyListener())
			cmf.cccb.UpdateClusterConfig(clusters())
			cmf.chcb.UpdateClusterHost(TestCluster, 0, hosts())

			meshReadyChan <- true

			srv.Start()

			select {
			case <-stopChan:
				srv.Close()
			}
		}
	}()

	go func() {
		select {
		case <-meshReadyChan:
			// client
			remoteAddr, _ := net.ResolveTCPAddr("tcp", MeshServerAddr)
			cc := network.NewClientConnection(nil, remoteAddr, stopChan)
			cc.AddConnectionCallbacks(&clientConnCallbacks{      //ADD  connection callback
				cc: cc,
			})
			cc.Connect()
			cc.SetReadDisable(false)
			cc.FilterManager().AddReadFilter(&clientConnReadFilter{})

			select {
			case <-stopChan:
				cc.Close(types.NoFlush)
			}
		}
	}()

	select {
	case <-time.After(time.Second * 5):
		stopChan <- true
		fmt.Println("[MAIN]closing..")
	}
}

func tcpProxyListener() v2.ListenerConfig {
	return v2.ListenerConfig{
		Name:                 TestListener,
		Addr:                 MeshServerAddr,
		BindToPort:           true,
		ConnBufferLimitBytes: 1024 * 32,
	}
}

type clientConnCallbacks struct {
	cc types.Connection
}

func (ccc *clientConnCallbacks) OnEvent(event types.ConnectionEvent) {
	fmt.Printf("[CLIENT]connection event %s", string(event))
	fmt.Println()

	switch event {
	case types.Connected:
		time.Sleep(3 * time.Second)

		fmt.Println("[CLIENT]write 'hello' to remote server")

		buf := bytes.NewBufferString("hello")
		ccc.cc.Write(buf)
	}
}

func (ccc *clientConnCallbacks) OnAboveWriteBufferHighWatermark() {}

func (ccc *clientConnCallbacks) OnBelowWriteBufferLowWatermark() {}

type clientConnReadFilter struct {
}

func (ccrf *clientConnReadFilter) OnData(buffer *bytes.Buffer) types.FilterStatus {
	fmt.Printf("[CLIENT]receive data '%s'", buffer.String())
	fmt.Println()
	buffer.Reset()

	return types.Continue
}

func (ccrf *clientConnReadFilter) OnNewConnection() types.FilterStatus {
	return types.Continue
}

func (ccrf *clientConnReadFilter) InitializeReadFilterCallbacks(cb types.ReadFilterCallbacks) {}

func tcpProxyConfig() *v2.TcpProxy {
	tcpProxyConfig := &v2.TcpProxy{}
	tcpProxyConfig.Routes = append(tcpProxyConfig.Routes, &v2.TcpRoute{
		Cluster: TestCluster,
	})

	return tcpProxyConfig
}

type clusterManagerFilter struct {
	cccb server.ClusterConfigFactoryCb
	chcb server.ClusterHostFactoryCb
}

func (cmf *clusterManagerFilter) OnCreated(cccb server.ClusterConfigFactoryCb, chcb server.ClusterHostFactoryCb) {
	cmf.cccb = cccb
	cmf.chcb = chcb
}

func clusters() []v2.Cluster {
	var configs []v2.Cluster
	configs = append(configs, v2.Cluster{
		Name:                 TestCluster,
		ClusterType:          v2.SIMPLE_CLUSTER,
		LbType:               v2.LB_RANDOM,
		MaxRequestPerConn:    1024,
		ConnBufferLimitBytes: 16 * 1026,
	})

	return configs
}

func hosts() []v2.Host {
	var hosts []v2.Host

	hosts = append(hosts, v2.Host{
		Address: RealServerAddr,
		Weight:  100,
	})

	return hosts
}
