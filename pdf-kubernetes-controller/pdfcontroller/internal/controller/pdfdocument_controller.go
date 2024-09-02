/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"encoding/base64"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	examplecomv1 "github.com/anishbista60/api/v1"
	"github.com/go-logr/logr"
)

// PdfDocumentReconciler reconciles a PdfDocument object
type PdfDocumentReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=example.com.my.domain,resources=pdfdocuments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=example.com.my.domain,resources=pdfdocuments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=example.com.my.domain,resources=pdfdocuments/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PdfDocument object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *PdfDocumentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("pdfdocument", req.NamespacedName)
	var pdfdoc examplecomv1.PdfDocument
	if err := r.Get(ctx, req.NamespacedName, &pdfdoc); err != nil {
		log.Error(err, "unable to fectch PdfDocument")
		return ctrl.Result{}, err
	}
	jobspec, err := r.createJob(pdfdoc)
	if err != nil {
		log.Error(err, "Erro in  creating the new jobs")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if err := r.Create(ctx, &jobspec); err != nil {
		log.Error(err, "uanable to create the job")
	}
	return ctrl.Result{}, nil
}

func (r *PdfDocumentReconciler) createJob(pdfdoc examplecomv1.PdfDocument) (batchv1.Job, error) {
	image := "knsit/pandoc"
	base64text := base64.StdEncoding.EncodeToString([]byte(pdfdoc.Spec.Text))

	j := batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			APIVersion: batchv1.SchemeGroupVersion.Group,
			Kind:       "Job",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      pdfdoc.Name + "-job",
			Namespace: pdfdoc.Namespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					InitContainers: []corev1.Container{
						{
							Name:    "store-in-md",
							Image:   "alpine",
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", fmt.Sprintf("echo %s | base64 -d >> /data/text.md", base64text)},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data-volume",
									MountPath: "/data",
								},
							},
						},
						{
							Name:    "convert",
							Image:   image,
							Command: []string{"sh", "-c"},
							Args:    []string{"-c", fmt.Sprintf("pandoc -s -o /data/%s.pdf /data/text.md", pdfdoc.Spec.DocumentName)},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data-volume",
									MountPath: "/data",
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "main",
							Image:   "alpine",
							Command: []string{"sh", "-c", "sleep 3600"},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data-volume",
									MountPath: "/data",
								},
							},
						},
					},
					Volumes: []corev1.Volume{

						{
							Name:         "data-volume",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
					},
				},
			},
		},
	}
	return j, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PdfDocumentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&examplecomv1.PdfDocument{}).
		Complete(r)
}
