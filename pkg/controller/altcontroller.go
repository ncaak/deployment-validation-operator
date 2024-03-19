package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
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
	// discovery discovery.DiscoveryInterface
	logger logr.Logger
	// cmWatcher             *configmap.Watcher
	// validationEngine      validations.Interface
	// apiResources          []metav1.APIResource
	//dynClient *dynamic.DynamicClient
	nmCache   map[string]string
	resources *resourceSet
}

func NewAltReconciler(
	client client.Client,
	cfg *rest.Config,
	// discoveryClient discovery.DiscoveryInterface,
	// cmw *configmap.Watcher,
	// validationEngine validations.Interface
	// apiResourceLists []*v1.APIResourceList,
) (*AltReconciler, error) {
	// listLimit, err := getListLimit()
	// if err != nil {
	// 	return nil, err
	// }

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("initializing clientset: %w", err)
	}

	// resourceSet := newResourceSet(client.Scheme())

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("initializing discovery client: %w", err)
	}

	// _, apiResourceLists, err := clientset.DiscoveryClient.ServerGroupsAndResources()
	_, apiResourceLists, err := discoveryClient.ServerGroupsAndResources()
	if err != nil {
		return nil, fmt.Errorf("getting ServerGroupsAndResources: %w", err)
	}

	set := newResourceSet(client.Scheme())
	err = getAvailableResourceSet(set, apiResourceLists)
	if err != nil {
		return nil, fmt.Errorf("getting getAvailableResourceSet: %w", err)
	}

	// dc, err := dynamic.NewForConfig(cfg)
	// if err != nil {
	// 	return nil, fmt.Errorf("initializing dynamic client: %w", err)
	// }

	// apiResources, err := reconcileResourceList(gr.discovery, gr.client.Scheme())
	// if err != nil {
	// 	gr.logger.Error(err, "retrieving API resources to reconcile")
	// 	return
	// }
	// gr.apiResources = apiResources

	return &AltReconciler{
		clientset: clientset,
		client:    client,
		// discovery: discoveryClient,
		// listLimit:             listLimit,
		watchNamespaces: newWatchNamespacesCache(),
		// objectValidationCache: newValidationCache(),
		// currentObjects:        newValidationCache(),
		logger: ctrl.Log.WithName("asdasdasd"),
		// cmWatcher:             cmw,
		// validationEngine:      validationEngine,
		//dynClient: dc,
		resources: set,
		nmCache:   make(map[string]string),
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

			gvkResources := alt.getNamespacedResourcesGVK(alt.resources.ToSlice())

			// Get the labels of the Deployment
			deploymentLabels := newCm.GetLabels()

			// Create a label selector for the Deployment labels
			selector := labels.Set(deploymentLabels).AsSelector()

			// resource := schema.GroupVersionResource{
			// 	Group:    newCm.GetObjectKind().GroupVersionKind().Group,
			// 	Version:  newCm.GetResourceVersion(),
			// 	Resource: "deployments",
			// }
			// resource1 := newCm.GroupVersionKind().GroupVersion().WithResource("deployments")
			// MatchLabels := newCm.Spec.Selector.MatchLabels

			// dc, err := alt.dynClient.Resource(
			// 	resource).Namespace(newCm.Namespace).List(
			// 	context.Background(), metav1.ListOptions{LabelSelector: selector.String()},
			// )
			// if err != nil {
			// 	alt.logger.Error(err, "trying to retrieve Deployment resources", "resource", resource)
			// }

			for _, gvk := range gvkResources {

				list := unstructured.UnstructuredList{}
				list.SetGroupVersionKind(gvk)
				listOptions := &client.ListOptions{
					Namespace:     newCm.Namespace,
					LabelSelector: selector,
				}
				if err := alt.client.List(ctx, &list, listOptions); err != nil {
					alt.logger.Error(err, "trying to retrieve Deployment resources")
					return
				}
				alt.logger.Info("deployment resources", "gvk", gvk, "resources", len(list.Items))
			}
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

func getAvailableResourceSet(set *resourceSet, resList []*v1.APIResourceList) error {
	// func reconcileResourceList(client discovery.DiscoveryInterface,
	// scheme *runtime.Scheme) ([]metav1.APIResource, error) {
	// set := newResourceSet(alt.client.Scheme())

	// _, apiResourceLists, err := alt.clientset.DiscoveryClient.ServerGroupsAndResources()
	// if err != nil {
	// 	return err
	// }

	for _, apiResourceList := range resList {
		gv, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
		if err != nil {
			return err
		}
		for _, rsc := range apiResourceList.APIResources {
			rsc.Group, rsc.Version = gv.Group, gv.Version
			key := schema.GroupKind{
				Group: gv.Group,
				Kind:  rsc.Kind,
			}
			if err := set.Add(key, rsc); err != nil {
				return fmt.Errorf("adding resource %s to set: %w", rsc.String(), err)
			}
		}
	}

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

func (alt *AltReconciler) getNamespacedResourcesGVK(resources []metav1.APIResource) []schema.GroupVersionKind {
	namespacedResources := make([]schema.GroupVersionKind, 0)
	for _, resource := range resources {
		if resource.Namespaced {
			gvk := gvkFromMetav1APIResource(resource)
			namespacedResources = append(namespacedResources, gvk)
		}
	}
	return namespacedResources
}
