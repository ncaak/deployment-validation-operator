package controller

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/app-sre/deployment-validation-operator/pkg/validations"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
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

// AltReconciler watches a defined object
type AltReconciler struct {
	watchNamespaces  *watchNamespacesCache
	client           client.Client
	clientset        *kubernetes.Clientset
	logger           logr.Logger
	validationEngine validations.Interface
	nmCache          map[string]string
	resources        *resourceSet
}

func NewAltReconciler(
	client client.Client,
	cfg *rest.Config,
	validationEngine validations.Interface,
) (*AltReconciler, error) {

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

	return &AltReconciler{
		clientset:        clientset,
		client:           client,
		watchNamespaces:  newWatchNamespacesCache(),
		logger:           ctrl.Log.WithName("AltReconciler"),
		validationEngine: validationEngine,
		resources:        set,
		nmCache:          make(map[string]string),
	}, nil
}

func (alt *AltReconciler) setWatchers(ctx context.Context) error {
	// Retrieves valid namespaces
	namespaces, err := alt.watchNamespaces.getWatchNamespaces(ctx, alt.client)
	if err != nil {
		return fmt.Errorf("getting watched namespaces: %w", err)
	}

	// Sets watchers over every Deployment object inside the namespace
	// Also, sets a cache of namespaces to avoid duplicated watchers
	for _, namespace := range *namespaces {
		alt.logger.Info("new namespace", "id", namespace.uid, "name", namespace.name)
		_, exists := alt.nmCache[namespace.uid]
		if !exists {
			alt.logger.Info("new namespace", "name", namespace.name)
			alt.nmCache[namespace.uid] = namespace.name
			alt.checkDeployments(ctx, namespace.name)
		}
	}

	return nil
}

func (alt *AltReconciler) checkDeployments(ctx context.Context, namespace string) error {

	// Sets functions to the watchers, Currently only using Add as it gets triggered once
	factory := informers.NewSharedInformerFactoryWithOptions(
		alt.clientset, time.Second*30, informers.WithNamespace(namespace),
	)

	informer := factory.Apps().V1().Deployments().Informer()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{ // nolint:errcheck
		AddFunc: func(obj interface{}) {
			newCm := obj.(*appsv1.Deployment)

			alt.logger.Info("new deployment", "name", newCm.Name, "namespace", namespace)

			// Gets GVK of only namespaced resources
			// alt.resources works as a dictionary of types of objects available in the cluster
			gvkResources := alt.getNamespacedResourcesGVK(alt.resources.ToSlice())

			// Get the labels of the Deployment
			// This is not being used now, as test templates only use selector
			// But it could be used as in the groupAppObjects function (generic reconciler)
			// deploymentLabels := newCm.GetLabels()
			// alt.logger.Info("deployment labels", "labels", deploymentLabels)

			// Create a label selector for the Deployment labels
			selector := labels.Set(newCm.Spec.Selector.MatchLabels).AsSelector()
			alt.logger.Info("deployment labels", "selector", selector)

			objects := make([]*unstructured.Unstructured, 0)
			for _, gvk := range gvkResources {

				list := unstructured.UnstructuredList{}
				list.SetGroupVersionKind(gvk)
				listOptions := &client.ListOptions{
					Namespace:     newCm.Namespace,
					LabelSelector: selector,
				}
				// Gets only objects matching the labels/selector of the Deployment
				// No need of filtering out objects or matching manually for label/selector
				if err := alt.client.List(ctx, &list, listOptions); err != nil {
					alt.logger.Error(err, "trying to retrieve Deployment resources")
					return
				}
				if len(list.Items) > 0 {
					for _, obj := range list.Items {
						objects = append(objects, &obj)
					}
				}

				//alt.logger.Info("deployment resources", "gvk", gvk, "resources", len(list.Items))
			}

			// Copied from the generic reconciler, normalizing the objects with type
			cliObjects := make([]client.Object, 0, len(objects))
			for _, o := range objects {
				typedClientObject, err := alt.unstructuredToTyped(o)
				if err != nil {
					alt.logger.Error(err, "instantiating typed object:")
					return
				}
				cliObjects = append(cliObjects, typedClientObject)
			}

			// Running the validations
			outcome, err := alt.validationEngine.RunValidationsForObjects(cliObjects, "99ffdd0b-a4c4-42e3-89c7-3d99d91d4d2b")
			if err != nil {
				alt.logger.Error(err, "running validations")
				return
			}
			alt.logger.Info("deployment resources", "objects", len(objects), "outcome", outcome)

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
			if err := alt.setWatchers(ctx); err != nil {
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

func (alt *AltReconciler) unstructuredToTyped(obj *unstructured.Unstructured) (client.Object, error) {
	typedResource, err := alt.lookUpType(obj)
	if err != nil {
		return nil, fmt.Errorf("looking up object type: %w", err)
	}

	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, typedResource); err != nil {
		return nil, fmt.Errorf("converting unstructured to typed object: %w", err)
	}

	return typedResource.(client.Object), nil
}

func (alt *AltReconciler) lookUpType(obj *unstructured.Unstructured) (runtime.Object, error) {
	gvk := obj.GetObjectKind().GroupVersionKind()
	typedObj, err := alt.client.Scheme().New(gvk)
	if err != nil {
		return nil, fmt.Errorf("creating new object of type %s: %w", gvk, err)
	}

	return typedObj, nil
}
