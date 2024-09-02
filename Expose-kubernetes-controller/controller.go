package main

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsinformers "k8s.io/client-go/informers/apps/v1"
	coreinformers "k8s.io/client-go/informers/core/v1"
	netinformers "k8s.io/client-go/informers/networking/v1"
	"k8s.io/client-go/kubernetes"
	appslisters "k8s.io/client-go/listers/apps/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	netlisters "k8s.io/client-go/listers/networking/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

type controller struct {
	clientset      kubernetes.Interface
	depLister      appslisters.DeploymentLister
	svcLister      corelisters.ServiceLister
	ingressLister  netlisters.IngressLister
	depCacheSynced cache.InformerSynced
	queue          workqueue.TypedRateLimitingInterface[interface{}]
}

func newController(
	clientset kubernetes.Interface,
	depInformer appsinformers.DeploymentInformer,
	svcInformer coreinformers.ServiceInformer,
	ingressInformer netinformers.IngressInformer) *controller {

	c := &controller{
		clientset:      clientset,
		depLister:      depInformer.Lister(),
		svcLister:      svcInformer.Lister(),
		ingressLister:  ingressInformer.Lister(),
		depCacheSynced: depInformer.Informer().HasSynced,
		queue:          workqueue.NewNamedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[interface{}](), "anish"),
	}

	depInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    c.handleAdd,
			DeleteFunc: c.handleDel,
		},
	)

	svcInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			DeleteFunc: c.handleSvcDel,
		},
	)

	ingressInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			DeleteFunc: c.handleIngressDel,
		},
	)

	return c
}

func (c *controller) run(ch <-chan struct{}) {
	fmt.Println("starting controller")
	if !cache.WaitForCacheSync(ch, c.depCacheSynced) {
		fmt.Print("waiting for cache to be synced\n")
		return
	}

	go func() {
		for c.processNextItem() {
		}
	}()

	<-ch
}

func (c *controller) processNextItem() bool {
	item, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(item)

	key, ok := item.(string)
	if !ok {
		fmt.Printf("expected string in work queue but got %#v\n", item)
		return true
	}

	err := c.syncDeployment(key)
	if err != nil {
		fmt.Printf("error syncing '%s': %s\n", key, err.Error())
		c.queue.AddRateLimited(key)
		return true
	}

	c.queue.Forget(item)
	return true
}

func (c *controller) syncDeployment(key string) error {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return err
	}

	// Get the deployment
	dep, err := c.depLister.Deployments(ns).Get(name)
	if apierrors.IsNotFound(err) {
		return c.cleanupResources(ns, name)
	}

	// Ensure Service exists
	_, err = c.svcLister.Services(ns).Get(name)
	if apierrors.IsNotFound(err) {
		fmt.Printf("Creating service %s/%s\n", ns, name)
		err = c.createService(context.Background(), dep)
		if err != nil {
			fmt.Printf("Error %s creating the ingress: %s\n", err.Error(), name)
		}
	}

	// Ensure Ingress exists
	_, err = c.ingressLister.Ingresses(ns).Get(name)
	if apierrors.IsNotFound(err) {
		fmt.Printf("Creating ingress %s/%s\n", ns, name)
		err = c.createIngress(context.Background(), ns, name)
		if err != nil {
			fmt.Printf("Error %s creating the ingress: %s\n", err.Error(), name)
		}
	}

	return nil
}

func (c *controller) createService(ctx context.Context, dep *appsv1.Deployment) error {
	svc := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dep.Name,
			Namespace: dep.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: dep.Spec.Template.Labels,
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: 80,
				},
			},
		},
	}
	_, err := c.clientset.CoreV1().Services(dep.Namespace).Create(ctx, &svc, metav1.CreateOptions{})
	return err
}

func (c *controller) createIngress(ctx context.Context, namespace, name string) error {
	ingress := netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/rewrite-target": "/",
			},
		},
		Spec: netv1.IngressSpec{
			IngressClassName: newString("nginx"),
			Rules: []netv1.IngressRule{
				{
					Host: "k8scontroller.anishbista.xyz",
					IngressRuleValue: netv1.IngressRuleValue{
						HTTP: &netv1.HTTPIngressRuleValue{
							Paths: []netv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: newPathType(netv1.PathTypePrefix),
									Backend: netv1.IngressBackend{
										Service: &netv1.IngressServiceBackend{
											Name: name,
											Port: netv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	_, err := c.clientset.NetworkingV1().Ingresses(namespace).Create(ctx, &ingress, metav1.CreateOptions{})
	return err
}

func (c *controller) cleanupResources(ns, name string) error {
	ctx := context.Background()

	// Attempt to delete Service
	if err := c.clientset.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			fmt.Printf("Error %s deleting service %s\n", err, name)
			return err
		}
	}

	// Attempt to delete Ingress
	if err := c.clientset.NetworkingV1().Ingresses(ns).Delete(ctx, name, metav1.DeleteOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			fmt.Printf("Error %s deleting the ingress %s\n", err, name)
			return err
		}
	}

	return nil
}

func (c *controller) handleAdd(obj interface{}) {
	fmt.Println("add was called")
	c.enqueue(obj)
}

func (c *controller) handleDel(obj interface{}) {
	fmt.Println("del was called")
	c.enqueue(obj)
}

func (c *controller) handleSvcDel(obj interface{}) {
	fmt.Println("service deletion detected")
	c.enqueue(obj)
}

func (c *controller) handleIngressDel(obj interface{}) {
	fmt.Println("ingress deletion detected")
	c.enqueue(obj)
}

func newString(name string) *string {
	return &name
}

func newPathType(pathType netv1.PathType) *netv1.PathType {
	return &pathType
}
func (c *controller) enqueue(obj interface{}) {
	key, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		fmt.Printf("getting key from cache %s\n", err.Error())
		return
	}
	c.queue.Add(key)
}
