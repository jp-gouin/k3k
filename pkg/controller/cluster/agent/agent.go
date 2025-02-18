package agent

import (
	"github.com/rancher/k3k/pkg/apis/k3k.io/v1alpha1"
	"github.com/rancher/k3k/pkg/controller"
	ctrlruntimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	configName = "agent-config"
)

type Agent interface {
	Name() string
	Config() ctrlruntimeclient.Object
	Resources() ([]ctrlruntimeclient.Object, error)
}

func New(cluster *v1alpha1.Cluster, serviceIP, sharedAgentImage, sharedAgentImagePullPolicy, token string) Agent {
	if cluster.Spec.Mode == VirtualNodeMode {
		return NewVirtualAgent(cluster, serviceIP, token)
	}
	return NewSharedAgent(cluster, serviceIP, sharedAgentImage, sharedAgentImagePullPolicy, token)
}

func configSecretName(clusterName string) string {
	return controller.SafeConcatNameWithPrefix(clusterName, configName)
}
