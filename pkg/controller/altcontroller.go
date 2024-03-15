package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	cache "k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// GenericReconciler watches a defined object
type AltReconciler struct {
	// conf                  *rest.Config
	// listLimit             int64
	watchNamespaces *watchNamespacesCache
	// objectValidationCache *validationCache
	// currentObjects        *validationCache
	client    client.Client
	clientset *kubernetes.Clientset
	// discovery             discovery.DiscoveryInterface
	logger logr.Logger
	// cmWatcher             *configmap.Watcher
	// validationEngine      validations.Interface
	// apiResources          []metav1.APIResource
	nmCache map[string]string
}

func NewAltReconciler(
	client client.Client,
	cfg *rest.Config,
	// discovery discovery.DiscoveryInterface,
	// cmw *configmap.Watcher,
	// validationEngine validations.Interface
) (*AltReconciler, error) {
	// listLimit, err := getListLimit()
	// if err != nil {
	// 	return nil, err
	// }

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("initializing clientset: %w", err)
	}

	return &AltReconciler{
		clientset: clientset,
		client:    client,
		// discovery:             discovery,
		// listLimit:             listLimit,
		watchNamespaces: newWatchNamespacesCache(),
		// objectValidationCache: newValidationCache(),
		// currentObjects:        newValidationCache(),
		logger: ctrl.Log.WithName("asdasdasd"),
		// cmWatcher:             cmw,
		// validationEngine:      validationEngine,
		nmCache: make(map[string]string),
	}, nil
}

func (alt *AltReconciler) asdasd(ctx context.Context) error {
	namespaces, err := alt.watchNamespaces.getWatchNamespaces(ctx, alt.client)
	if err != nil {
		return fmt.Errorf("getting watched namespaces: %w", err)
	}

	alt.logger.Info("namespaces")
	for _, namespace := range *namespaces {
		// alt.logger.Info("new namespace", "id", namespace.uid, "name", namespace.name)
		_, exists := alt.nmCache[namespace.uid]
		if !exists {
			alt.logger.Info("new namespace", "name", namespace.name)
			alt.nmCache[namespace.uid] = namespace.name
			alt.checkDeployments(ctx, namespace.name)
		}
	}

	// alt.logger.Info("map namespaces", "map", alt.nmCache)
	alt.logger.Info("/////new namespaces")
	return nil
}

func (alt *AltReconciler) checkDeployments(ctx context.Context, namespace string) error {
	// deployments, err := alt.clientset.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	// if err != nil {
	// 	return fmt.Errorf("getting deplyoments: %w", err)
	// }

	// fmt.Printf("deployments: %v\n", deployments)
	// return nil
	factory := informers.NewSharedInformerFactoryWithOptions(
		alt.clientset, time.Second*30, informers.WithNamespace(namespace),
	)

	informer := factory.Apps().V1().Deployments().Informer()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{ // nolint:errcheck
		AddFunc: func(obj interface{}) {
			newCm := obj.(*appsv1.Deployment)

			alt.logger.Info("new deployment", "name", newCm.Name, "namespace", namespace)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			newCm := newObj.(*appsv1.Deployment)

			// This is sometimes triggered even if no change was due to the ConfigMap
			if reflect.DeepEqual(oldObj, newObj) {
				return
			}

			alt.logger.Info("existing deployment", "name", newCm.Name, "namespace", namespace)
		},
		DeleteFunc: func(oldObj interface{}) {
			newCm := oldObj.(*appsv1.Deployment)

			alt.logger.Info("deleting deployment", "name", newCm.Name, "namespace", namespace)
		},
	})

	factory.Start(ctx.Done())

	return nil
}

// AddToManager will add the reconciler for the configured obj to a manager.
func (alt *AltReconciler) AddToManager(mgr manager.Manager) error {
	return mgr.Add(alt)
}

// Start validating the given object kind every interval.
func (alt *AltReconciler) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			// stop reconciling
			return nil
		default:
			time.Sleep(10 * time.Second)
			if err := alt.asdasd(ctx); err != nil {
				alt.logger.Error(err, "error fetching and validating resource types")
			}
		}
	}
}
