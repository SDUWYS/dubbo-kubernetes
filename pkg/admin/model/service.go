/*
 * Licensed to the Apache Software Foundation (ASF) under one or more
 * contributor license agreements.  See the NOTICE file distributed with
 * this work for additional information regarding copyright ownership.
 * The ASF licenses this file to You under the Apache License, Version 2.0
 * (the "License"); you may not use this file except in compliance with
 * the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package model

import (
	"github.com/apache/dubbo-kubernetes/api/mesh/v1alpha1"
	core_mesh "github.com/apache/dubbo-kubernetes/pkg/core/resources/apis/mesh"
	"strings"
)

type ServiceSearchResp struct {
	ServiceName   string         `json:"serviceName"`
	VersionGroups []VersionGroup `json:"versionGroups"`
}

type ServiceSearch struct {
	ServiceName   string
	VersionGroups Set
}

func (s *ServiceSearch) FromServiceInfo(info *v1alpha1.ServiceInfo) {
	s.VersionGroups.Add(info.Group + " " + info.Version)
}

func NewServiceSearch(serviceName string) *ServiceSearch {
	return &ServiceSearch{
		ServiceName:   serviceName,
		VersionGroups: NewSet(),
	}
}

func NewServiceSearchResp() *ServiceSearchResp {
	return &ServiceSearchResp{
		ServiceName:   "",
		VersionGroups: nil,
	}
}

func NewServiceDistributionResp() *ServiceTabDistributionResp {
	return &ServiceTabDistributionResp{
		AppName:      "",
		InstanceName: "",
		Endpoint:     "",
		TimeOut:      "",
		Retries:      "",
	}
}

func (s *ServiceSearchResp) FromServiceSearch(search *ServiceSearch) {
	s.ServiceName = search.ServiceName
	versionGroupList := make([]VersionGroup, 0)
	for _, gv := range search.VersionGroups.Values() {
		groupAndVersion := strings.Split(gv, " ")
		versionGroupList = append(versionGroupList, VersionGroup{Version: groupAndVersion[0], Group: groupAndVersion[1]})
	}
	s.VersionGroups = versionGroupList
}

type ServiceTabDistributionReq struct {
	ServiceName string `json:"serviceName"`
	Version     string `json:"version"`
	Group       string `json:"group"`
}

type ServiceTabDistributionResp struct {
	AppName      string `json:"appName"`
	InstanceName string `json:"instanceName"`
	Endpoint     string `json:"endpoint"`
	TimeOut      string `json:"timeOut"`
	Retries      string `json:"retries"`
}

type ServiceTabDistribution struct {
	AppName      string
	InstanceName string
	Endpoint     string
	TimeOut      string
	Retries      string
}

func (d *ServiceTabDistribution) mergeInbound(inbound *v1alpha1.Dataplane_Networking_Inbound, ip string) {
	d.Endpoint = ip + string(inbound.Port)
}

func NewServiceDistribution() *ServiceTabDistribution {
	return &ServiceTabDistribution{
		AppName:      "",
		InstanceName: "",
		Endpoint:     "",
		TimeOut:      "",
		Retries:      "",
	}
}

func (r *ServiceTabDistributionResp) FromServiceDataplaneResource(dataplane *core_mesh.DataplaneResource, metadatalist *core_mesh.MetaDataResourceList, name string, req *ServiceTabDistributionReq) *ServiceTabDistributionResp {
	r.AppName = name
	inbounds := dataplane.Spec.Networking.Inbound
	ip := dataplane.GetIP()
	for _, inbound := range inbounds {
		r.mergeInbound(inbound, ip)
	}
	meta := dataplane.GetMeta()
	r.InstanceName = meta.GetName()
	r.mergeMetaData(metadatalist, req)

	return r

}

func (r ServiceTabDistributionResp) mergeInbound(inbound *v1alpha1.Dataplane_Networking_Inbound, ip string) {
	r.Endpoint = ip + string(inbound.Port)
}

func (r *ServiceTabDistributionResp) FromServiceDistribution(distribution *ServiceTabDistribution) *ServiceTabDistributionResp {
	r.AppName = distribution.AppName
	r.InstanceName = distribution.InstanceName
	r.Endpoint = distribution.Endpoint
	r.TimeOut = distribution.TimeOut
	r.Retries = distribution.Retries
	return r
}

func (r *ServiceTabDistributionResp) mergeMetaData(metadatalist *core_mesh.MetaDataResourceList, req *ServiceTabDistributionReq) {
	for _, metadata := range metadatalist.Items {
		// key format is '{group}/{interface name}:{version}:{protocol}'
		serviceinfos := metadata.Spec.Services
		for _, serviceinfo := range serviceinfos {
			if serviceinfo.Name == req.ServiceName &&
				serviceinfo.Group == req.Group &&
				serviceinfo.Version == req.Version {
				r.Retries = serviceinfo.Params["retries"]
				r.TimeOut = serviceinfo.Params["timeOut"]
			}
		}

	}
}

type VersionGroup struct {
	Version string `json:"version"`
	Group   string `json:"group"`
}
