// Copyright 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Binary ocbgpdemo provides a demonstration application which uses the OpenConfig
// structs library to create a data instance of the BGP model, and output it as
// JSON.
package main

import (
	"fmt"

	log "github.com/golang/glog"
	oc "github.com/michaelhenkel/ygot/exampleoc"
	"github.com/michaelhenkel/ygot/ygot"
)

func main() {
	bgp, err := CreateDemoBGPInstance()
	if err != nil {
		log.Exitf("Error in OpenConfig BGP demo: %v", err)
	}
	json, err := EmitBGPJSON(bgp)
	if err != nil {
		log.Exitf("Error outputting JSON: %v", err)
	}
	fmt.Println(json)

	ietfjson, err := EmitRFC7951JSON(bgp)
	if err != nil {
		log.Exitf("Error outputting RFC7951 JSON: %v", err)
	}
	fmt.Println(ietfjson)
}

// CreateDemoBGPInstance creates a demo OpenConfig BGP instance using the legacy
// BGP path at /bgp. It is specifically created as a separate function in order
// to be used as a regression test for the chain of Go struct
// generation from the OpenConfig YANG schema. Returns the GoStruct that is constructed
// rather than the JSON so that different rendering methods can be used.
func CreateDemoBGPInstance() (*oc.Bgp, error) {
	// Initialise the OpenConfig BGP model at /bgp (pre-network instance). The
	// struct is named according to the path of the entity ignoring any "stuttering"
	// or "config" and "state" within the path.
	bgp := &oc.Bgp{
		Global: &oc.Bgp_Global{
			As:       ygot.Uint32(15169),
			RouterId: ygot.String("192.0.2.42"),
		},
	}

	// NewNeighbor is an autogenerated method that adds a new neighbor using the
	// relevant key to the neighbor list. The key is typed according to the
	// native type in the schema. When there is a compound key then a struct that
	// just represents the key is provided, which is also typed.
	nPeer, err := bgp.NewNeighbor("2001:DB8::1")
	if err != nil {
		log.Errorf("neighbor 2001:bd8::1 already existed: %v", err)
	}
	// The returned struct represents the new peer that was created.
	nPeer.PeerAs = ygot.Uint32(29636)
	nPeer.Description = ygot.String("catalyst2 Services Ltd")
	nPeer.PeerType = oc.OpenconfigBgp_PeerType_INTERNAL

	// Initialize all containers underneath the newly created peer. This allows us
	// to now specify containers in the hierarchy that we didn't yet initialize.
	ygot.BuildEmptyTree(nPeer)
	nPeer.Timers.HoldTime = ygot.Float64(30)

	// Elements of the schema that themselves are containers have a struct
	// generated for them which can be set directly.
	nPeer.Timers = &oc.Bgp_Neighbor_Timers{
		HoldTime:          ygot.Float64(90.0),
		KeepaliveInterval: ygot.Float64(30.0),
	}

	// An entry in a list can be directly defined as a map entry, with the multi-key
	// level struct specified directly.
	bgp.Neighbor["192.0.2.1"] = &oc.Bgp_Neighbor{
		PeerAs:          ygot.Uint32(2856),
		NeighborAddress: ygot.String("192.0.2.1"),
		Description:     ygot.String("BT UK"),
		Timers: &oc.Bgp_Neighbor_Timers{
			HoldTime:          ygot.Float64(30.0),
			KeepaliveInterval: ygot.Float64(10.0),
		},
		Transport: &oc.Bgp_Neighbor_Transport{
			PassiveMode: ygot.Bool(true),
		},
	}

	// Set the peer as a route reflector client using the To_Bgp_Neighbor_RouteReflector_RouteReflectorClusterId_Union
	// helper function.
	cid, err := (*oc.Bgp_Neighbor_RouteReflector)(nil).To_Bgp_Neighbor_RouteReflector_RouteReflectorClusterId_Union("10.0.1.2")
	if err != nil {
		return nil, err
	}

	bgp.Neighbor["192.0.2.1"].RouteReflector = &oc.Bgp_Neighbor_RouteReflector{
		RouteReflectorClusterId: cid,
	}

	return bgp, nil
}

// EmitBGPJSON outputs JSON using the ygot.EmitJSON function.
func EmitBGPJSON(bgp *oc.Bgp) (string, error) {
	// Outputting JSON is simply a case of calling the output library's EmitJSON
	// function.
	json, err := ygot.EmitJSON(bgp, nil)
	if err != nil {
		return "", err
	}
	return json, nil
}

// EmitRFC7951JSON outputs RFC7951-compliant JSON using the ygot.EmitJSON function.
func EmitRFC7951JSON(bgp *oc.Bgp) (string, error) {
	json, err := ygot.EmitJSON(bgp, &ygot.EmitJSONConfig{
		Format: ygot.RFC7951,
		RFC7951Config: &ygot.RFC7951JSONConfig{
			AppendModuleName: true,
		},
	})
	if err != nil {
		return "", err
	}
	return json, nil
}
