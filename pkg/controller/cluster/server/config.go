package server

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/rancher/k3k/pkg/apis/k3k.io/v1beta1"
	"github.com/rancher/k3k/pkg/controller"
)

func (s *Server) Config(init bool, serviceIP string) (*corev1.Secret, error) {
	name := configSecretName(s.cluster.Name, init)

	sans := sets.NewString(s.cluster.Spec.TLSSANs...)
	sans.Insert(
		serviceIP,
		ServiceName(s.cluster.Name),
		fmt.Sprintf("%s.%s", ServiceName(s.cluster.Name), s.cluster.Namespace),
	)

	s.cluster.Status.TLSSANs = sans.List()

	config := serverConfigData(serviceIP, s.cluster, s.token)
	if init {
		config = initConfigData(s.cluster, s.token)
	}

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.cluster.Namespace,
		},
		Data: map[string][]byte{
			"config.yaml": []byte(config),
		},
	}, nil
}

func serverConfigData(serviceIP string, cluster *v1beta1.Cluster, token string) string {
	return "cluster-init: true\nserver: https://" + serviceIP + "\n" + serverOptions(cluster, token)
}

func initConfigData(cluster *v1beta1.Cluster, token string) string {
	return "cluster-init: true\n" + serverOptions(cluster, token)
}

func serverOptions(cluster *v1beta1.Cluster, token string) string {
	var opts string

	// TODO: generate token if not found
	if token != "" {
		opts = "token: " + token + "\n"
	}

	if cluster.Status.ClusterCIDR != "" {
		opts = opts + "cluster-cidr: " + cluster.Status.ClusterCIDR + "\n"
	}

	if cluster.Status.ServiceCIDR != "" {
		opts = opts + "service-cidr: " + cluster.Status.ServiceCIDR + "\n"
	}

	if cluster.Spec.ClusterDNS != "" {
		opts = opts + "cluster-dns: " + cluster.Spec.ClusterDNS + "\n"
	}

	if len(cluster.Status.TLSSANs) > 0 {
		opts = opts + "tls-san:\n"
		for _, addr := range cluster.Status.TLSSANs {
			opts = opts + "- " + addr + "\n"
		}
	}

	// shared and hcp modes both run K3s with --disable-agent (agentless server).
	// hcp additionally relies on this to satisfy the PRD requirement that the
	// control plane never runs a kubelet and is not enumerated as a node.
	if cluster.Spec.Mode != v1beta1.VirtualClusterMode {
		opts = opts + "disable-agent: true\ndisable:\n- servicelb\n- traefik\n- metrics-server\n- local-storage\n"
	}

	// In shared mode workloads run on the host cluster, so the apiserver pod
	// can reach them directly via the host pod network and the egress
	// selector is unnecessary.
	//
	// In hcp mode the apiserver pod has NO route to the virtual cluster's
	// pod CIDR (which only exists on joined external worker nodes), and the
	// kube-apiserver bypasses kube-proxy when calling webhooks / proxying
	// to pods: it resolves Service -> Endpoints itself and dials the Pod IP
	// directly. We therefore tunnel apiserver egress through the WebSocket
	// each k3s-agent maintains back to the server.
	//
	// We pick "cluster" rather than "pod" or "agent" because the agent-side
	// authorizer differs by mode (k3s pkg/agent/tunnel/tunnel.go):
	//   - agent:   only kubelet calls are tunneled; pod-IP dials go direct
	//              and fail in HCP (no route to virtual pod CIDR).
	//   - pod:     authorizer only allows pod IPs the agent has *already
	//              watched*. A newly-created pod's IP is rejected with
	//              "connect not allowed", which terminates the entire
	//              remotedialer session and 502s in-flight kubelet streams
	//              -> kubectl logs / exec / webhooks become flaky.
	//   - cluster: authorizer pre-populates the cluster CIDR + node IPs as
	//              non-hostNet entries, so every pod IP and every node port
	//              is permitted. No race, no per-port allowlist. This is
	//              what we want for a managed control plane.
	switch cluster.Spec.Mode {
	case v1beta1.SharedClusterMode:
		opts = opts + "egress-selector-mode: disabled\n"
	case v1beta1.HCPClusterMode:
		opts = opts + "egress-selector-mode: cluster\n"
	}

	// In hcp mode the apiserver pod IP is unreachable from external worker
	// nodes, so the kube-apiserver's default lease-based endpoint reconciler
	// would publish a broken default/kubernetes Endpoints (advertise-address +
	// secure-port). Disable it so K3k can own that Endpoints object and point
	// it at the externally-reachable host:port (NodePort / LB / Ingress).
	if cluster.Spec.Mode == v1beta1.HCPClusterMode {
		opts = opts + "kube-apiserver-arg:\n- endpoint-reconciler-type=none\n"
	}
	// TODO: Add extra args to the options

	return opts
}

func configSecretName(clusterName string, init bool) string {
	if !init {
		return controller.SafeConcatNameWithPrefix(clusterName, configName)
	}

	return controller.SafeConcatNameWithPrefix(clusterName, initConfigName)
}
