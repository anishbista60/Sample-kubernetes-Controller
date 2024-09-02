package main

import (
	"flag"
	"fmt"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	kubeconfig := flag.String("kubeconfig", "/home/anishbista/.kube/config", "other location to the kube config file")

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)

	if err != nil {
		fmt.Printf("Error %v in building kubeconfig", err.Error())

		config, err = rest.InClusterConfig()
		if err != nil {
			fmt.Printf("Error %v in taking config", err.Error())
		}
	}

	clientset, err := kubernetes.NewForConfig(config)

	if err != nil {
		fmt.Printf("Error %v creating new client ", err.Error())
	}
	ch := make(chan struct{})
	informers := informers.NewSharedInformerFactory(clientset, 10*time.Minute)

	c := newController(clientset, informers.Apps().V1().Deployments(), informers.Core().V1().Services(), informers.Networking().V1().Ingresses())
	informers.Start(ch)
	c.run(ch)
}
