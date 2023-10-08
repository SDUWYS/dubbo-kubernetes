// Licensed to the Apache Software Foundation (ASF) under one or more
// contributor license agreements.  See the NOTICE file distributed with
// this work for additional information regarding copyright ownership.
// The ASF licenses this file to You under the Apache License, Version 2.0
// (the "License"); you may not use this file except in compliance with
// the License.  You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package patch

import (
	"fmt"
	"strconv"

	dubbo_cp "github.com/apache/dubbo-kubernetes/pkg/config/app/dubbo-cp"
	"github.com/apache/dubbo-kubernetes/pkg/core/client/webhook"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type JavaSdk struct {
	options       *dubbo_cp.Config
	webhookClient webhook.Client
	kubeClient    kubernetes.Interface
}

func NewJavaSdk(options *dubbo_cp.Config, webhookClient webhook.Client, kubeClient kubernetes.Interface) *JavaSdk {
	return &JavaSdk{
		options:       options,
		webhookClient: webhookClient,
		kubeClient:    kubeClient,
	}
}

const (
	ExpireSeconds           = 1800
	Labeled                 = "true"
	EnvDubboRegistryAddress = "DUBBO_REGISTRY_ADDRESS"
)

// the priority of registry
// default is zk > nacos
var (
	registryInjectPriorities = []string{
		"dubbo.apache.org/zookeeper",
		"dubbo.apache.org/nacos",
	}
	registrySchemas = map[string]string{
		"dubbo.apache.org/zookeeper": "zookeeper",
		"dubbo.apache.org/nacos":     "nacos",
	}

	prometheusAnnotations = map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/path":   "management/prometheus",
		"prometheus.io/port":   "18081",
	}
)

func (s *JavaSdk) NewPodWithDubboPrometheusInject(origin *v1.Pod) (*v1.Pod, error) {
	target := origin.DeepCopy()

	shouldInject := false
	if target.Labels["dubbo.apache.org/prometheus"] == Labeled { // find in pod labels
		shouldInject = true
	}
	if s.webhookClient.GetNamespaceLabels(target.Namespace)["dubbo.apache.org/prometheus"] == Labeled {
		// find in namespace labels
		shouldInject = true
	}
	if !shouldInject {
		return target, nil
	}

	serviceList := s.webhookClient.ListServices(target.Namespace, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", "dubbo.apache.org/prometheus", Labeled),
	})

	if serviceList == nil || len(serviceList.Items) < 1 {
		return target, nil
	}

	// inject prometheus into annotations
	s.injectAnnotations(target, prometheusAnnotations)

	return target, nil
}

func (s *JavaSdk) injectAnnotations(target *v1.Pod, annotations map[string]string) {
	if target.Annotations == nil {
		target.Annotations = make(map[string]string)
	}

	for k, v := range annotations {
		if _, ok := target.Annotations[k]; !ok {
			target.Annotations[k] = v
		}
	}
}

func (s *JavaSdk) NewPodWithDubboRegistryInject(origin *v1.Pod) (*v1.Pod, error) {
	target := origin.DeepCopy()

	// find specific registry inject label (such as zookeeper-registry-inject)
	// in pod labels and namespace labels
	var registryInjects []string
	for _, registryInject := range registryInjectPriorities {
		if target.Labels[registryInject] == Labeled { // find in pod labels
			registryInjects = []string{registryInject}
			break
		}

		if s.webhookClient.GetNamespaceLabels(target.Namespace)[registryInject] == Labeled {
			// find in namespace labels
			registryInjects = []string{registryInject}
			break
		}
	}
	if len(registryInjects) == 0 {
		registryInjects = registryInjectPriorities
	}

	// find registry service in k8s
	var registryAddress string
	for _, registryInject := range registryInjects {
		serviceList := s.webhookClient.ListServices(target.Namespace, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", registryInject, Labeled),
		})

		if serviceList == nil || len(serviceList.Items) < 1 {
			continue
		}

		schema := registrySchemas[registryInject]
		registryAddress = fmt.Sprintf("%s://%s.%s.svc", schema, serviceList.Items[0].Name, serviceList.Items[0].Namespace)
		break
	}

	var found bool
	if len(registryAddress) > 0 {
		// inject into env
		var targetContainers []v1.Container
		for _, c := range target.Spec.Containers {
			if !found { // found DUBBO_REGISTRY_ADDRESS ENV, stop inject
				found = s.injectEnv(&c, EnvDubboRegistryAddress, registryAddress)
			}

			targetContainers = append(targetContainers, c)
		}
		target.Spec.Containers = targetContainers
	}

	return target, nil
}

func (s *JavaSdk) injectEnv(container *v1.Container, name, value string) (found bool) {
	for j, env := range container.Env {
		if env.Name == name {
			found = true
			// env is not empty, inject into env
			if len(env.Value) > 0 {
				break
			}

			container.Env[j].Value = value
			break
		}
	}
	if found { // found registry env in pod, stop inject
		return
	}

	container.Env = append(container.Env, v1.EnvVar{
		Name:  name,
		Value: value,
	})

	return
}

func (s *JavaSdk) NewPodWithDubboCa(origin *v1.Pod) (*v1.Pod, error) {
	target := origin.DeepCopy()
	expireSeconds := int64(ExpireSeconds)

	shouldInject := false

	if target.Labels["dubbo-ca.inject"] == Labeled {
		shouldInject = true
	}

	if !shouldInject && s.webhookClient.GetNamespaceLabels(target.Namespace)["dubbo-ca.inject"] == Labeled {
		shouldInject = true
	}

	if shouldInject {
		shouldInject = s.checkVolume(target, shouldInject)

		for _, c := range target.Spec.Containers {
			shouldInject = s.checkContainers(c, shouldInject)
		}
	}

	if shouldInject {
		s.injectVolumes(target, expireSeconds)

		var targetContainers []v1.Container
		for _, c := range target.Spec.Containers {
			s.injectContainers(&c)

			targetContainers = append(targetContainers, c)
		}
		target.Spec.Containers = targetContainers
	}

	return target, nil
}

func (s *JavaSdk) injectContainers(c *v1.Container) {
	c.Env = append(c.Env, v1.EnvVar{
		Name:  "DUBBO_CA_ADDRESS",
		Value: s.options.KubeConfig.ServiceName + "." + s.options.KubeConfig.Namespace + ".svc:" + strconv.Itoa(s.options.GrpcServer.SecureServerPort),
	})
	c.Env = append(c.Env, v1.EnvVar{
		Name:  "DUBBO_CA_CERT_PATH",
		Value: "/var/run/secrets/dubbo-ca-cert/ca.crt",
	})
	c.Env = append(c.Env, v1.EnvVar{
		Name:  "DUBBO_OIDC_TOKEN",
		Value: "/var/run/secrets/dubbo-ca-token/token",
	})
	c.Env = append(c.Env, v1.EnvVar{
		Name:  "DUBBO_OIDC_TOKEN_TYPE",
		Value: "dubbo-ca-token",
	})

	c.VolumeMounts = append(c.VolumeMounts, v1.VolumeMount{
		Name:      "dubbo-ca-token",
		MountPath: "/var/run/secrets/dubbo-ca-token",
		ReadOnly:  true,
	})
	c.VolumeMounts = append(c.VolumeMounts, v1.VolumeMount{
		Name:      "dubbo-ca-cert",
		MountPath: "/var/run/secrets/dubbo-ca-cert",
		ReadOnly:  true,
	})
}

func (s *JavaSdk) injectVolumes(target *v1.Pod, expireSeconds int64) {
	target.Spec.Volumes = append(target.Spec.Volumes, v1.Volume{
		Name: "dubbo-ca-token",
		VolumeSource: v1.VolumeSource{
			Projected: &v1.ProjectedVolumeSource{
				Sources: []v1.VolumeProjection{
					{
						ServiceAccountToken: &v1.ServiceAccountTokenProjection{
							Audience:          "dubbo-ca",
							ExpirationSeconds: &expireSeconds,
							Path:              "token",
						},
					},
				},
			},
		},
	})
	target.Spec.Volumes = append(target.Spec.Volumes, v1.Volume{
		Name: "dubbo-ca-cert",
		VolumeSource: v1.VolumeSource{
			Projected: &v1.ProjectedVolumeSource{
				Sources: []v1.VolumeProjection{
					{
						ConfigMap: &v1.ConfigMapProjection{
							LocalObjectReference: v1.LocalObjectReference{
								Name: "dubbo-ca-cert",
							},
							Items: []v1.KeyToPath{
								{
									Key:  "ca.crt",
									Path: "ca.crt",
								},
							},
						},
					},
				},
			},
		},
	})
}

func (s *JavaSdk) checkContainers(c v1.Container, shouldInject bool) bool {
	for _, e := range c.Env {
		if e.Name == "DUBBO_CA_ADDRESS" {
			shouldInject = false
			break
		}
		if e.Name == "DUBBO_CA_CERT_PATH" {
			shouldInject = false
			break
		}
		if e.Name == "DUBBO_OIDC_TOKEN" {
			shouldInject = false
			break
		}
		if e.Name == "DUBBO_OIDC_TOKEN_TYPE" {
			shouldInject = false
			break
		}
	}

	for _, m := range c.VolumeMounts {
		if m.Name == "dubbo-ca-token" {
			shouldInject = false
			break
		}
		if m.Name == "dubbo-ca-cert" {
			shouldInject = false
			break
		}
	}
	return shouldInject
}

func (s *JavaSdk) checkVolume(target *v1.Pod, shouldInject bool) bool {
	for _, v := range target.Spec.Volumes {
		if v.Name == "dubbo-ca-token" {
			shouldInject = false
			break
		}
	}
	for _, v := range target.Spec.Volumes {
		if v.Name == "dubbo-ca-cert" {
			shouldInject = false
			break
		}
	}
	return shouldInject
}
