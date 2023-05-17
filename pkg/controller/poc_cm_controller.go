package controller

import (
	"context"
	"fmt"
	"reflect"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	_ manager.Runnable = &ConfigMapController{}
)

type ConfigMapController struct {
	cached map[string]string
}

func NewConfigMapController() *ConfigMapController {
	return &ConfigMapController{}
}

// Reconcile is called when a ConfigMap is updated or created
// The namespace/name is set when added to the Manager
func (c *ConfigMapController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	fmt.Printf("Configuration ConfigMap %s/%s", req.Namespace, req.Name)

	// Retrieving configuration ConfigMap
	gatherKubeClient, err := kubernetes.NewForConfig(ctrl.GetConfigOrDie())
	if err != nil {
		return ctrl.Result{}, nil
	}
	coreClient := gatherKubeClient.CoreV1()
	cfmp, err := coreClient.ConfigMaps(req.Namespace).Get(ctx, req.Name, v1.GetOptions{})
	if err != nil {
		return ctrl.Result{}, nil
	}

	// TODO
	//
	// Update validations
	//
	/////////

	// Update cache configuration for next changes
	if !reflect.DeepEqual(c.cached, cfmp.Data) {
		c.cached = cfmp.Data
	}
	return ctrl.Result{}, nil
}

// Runnable interface required by the manager
func (gr *ConfigMapController) Start(ctx context.Context) error {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				fmt.Print("Reconciling Configuration ConfigMap...")
			}
		}
	}()
	return nil
}

func (c *ConfigMapController) Stop() error {
	return nil
}
