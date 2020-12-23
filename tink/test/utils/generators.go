/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"sync"

	"github.com/tinkerbell/tink/protos/hardware"
	"github.com/tinkerbell/tink/protos/template"
)

const HelloWorldTemplate = `version: "0.1"
name: hello_world_workflow
global_timeout: 600
tasks:
  - name: "hello world"
    worker: "{{.device_1}}"
    actions:
      - name: "hello_world"
        image: hello-world
        timeout: 60`

// These are CIDRs that we should not come across in a real
// environment, since they are reserved for use in documentation
// and examples.
var testCIDRs = [...]string{
	"192.0.2.0/24",
	"198.51.100.0/24",
	"203.0.113.0/24",
}

var IPGetter = ipGetter{
	addresses: make(map[string]string),
}

type ipGetter struct {
	addresses map[string]string
	lock      sync.Mutex
}

func (i *ipGetter) nextAddressFromCIDR(cidr string) (string, string, string, error) {
	i.lock.Lock()
	defer i.lock.Unlock()

	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to parse cidr: %w", err)
	}

	netMask := net.IP(network.Mask).String()

	// Use the first available address as the gateway address
	gw := make(net.IP, len(network.IP))
	copy(gw, network.IP)
	gw[len(gw)-1]++
	gateway := gw.String()

	// Attempt to get the last address used, otherwise use the
	// gateway address as the starting point
	lastAddress, ok := i.addresses[cidr]
	if !ok {
		lastAddress = gateway
	}

	// Get the next IP by incrementing lastAddress
	nextIP := net.ParseIP(lastAddress)
	nextIP[len(nextIP)-1]++

	ip := nextIP.String()

	// Store the last address
	i.addresses[cidr] = ip

	return ip, netMask, gateway, nil
}

var MACGenerator = macGenerator{
	addresses: make(map[string]struct{}),
}

type macGenerator struct {
	addresses map[string]struct{}
	lock      sync.Mutex
}

func (m *macGenerator) Get() (string, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	for {
		mac := net.HardwareAddr(make([]byte, 6))

		_, err := rand.Read(mac)
		if err != nil {
			return "", fmt.Errorf("failed to generate random mac: %w", err)
		}

		// Ensure the individual bit is set
		mac[0] &= ^byte(1)

		// Ensure the local bit is set
		mac[0] |= byte(2)

		key := mac.String()
		if _, found := m.addresses[key]; !found {
			m.addresses[key] = struct{}{}

			return key, nil
		}
	}
}

func GenerateTemplate(name, data string) *template.WorkflowTemplate {
	return &template.WorkflowTemplate{
		Name: name,
		Data: data,
	}
}

func GenerateHardware(numInterfaces int) (*hardware.Hardware, error) {
	hw := &hardware.Hardware{
		Network: &hardware.Hardware_Network{},
	}

	for i := 0; i < numInterfaces; i++ {
		cidr := testCIDRs[i%len(testCIDRs)]

		ni, err := GenerateHardwareInterface(cidr)
		if err != nil {
			return nil, err
		}

		hw.Network.Interfaces = append(hw.Network.Interfaces, ni)
	}

	return hw, nil
}

func GenerateHardwareInterface(cidr string) (*hardware.Hardware_Network_Interface, error) {
	if cidr == "" {
		i, err := rand.Int(rand.Reader, big.NewInt(int64(len(testCIDRs))))
		if err != nil {
			return nil, fmt.Errorf("failed to get random index for cidr: %w", err)
		}

		cidr = testCIDRs[i.Int64()]
	}

	ip, netmask, gateway, err := IPGetter.nextAddressFromCIDR(cidr)
	if err != nil {
		return nil, err
	}

	mac, err := MACGenerator.Get()
	if err != nil {
		return nil, err
	}

	ni := &hardware.Hardware_Network_Interface{
		Dhcp: &hardware.Hardware_DHCP{
			Mac: mac,
			Ip: &hardware.Hardware_DHCP_IP{
				Address: ip,
				Netmask: netmask,
				Gateway: gateway,
			},
		},
	}

	return ni, nil
}
