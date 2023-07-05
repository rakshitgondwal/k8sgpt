/*
Copyright 2023 The K8sGPT Authors.
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

package analyzer

import (
	"context"
	"fmt"

	"github.com/k8sgpt-ai/k8sgpt/pkg/common"
	"github.com/k8sgpt-ai/k8sgpt/pkg/kubernetes"
	"github.com/k8sgpt-ai/k8sgpt/pkg/util"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type WebhookAnalyzer struct{}

func (WebhookAnalyzer) Analyze(a common.Analyzer) ([]common.Result, error) {

	kind := "Webhook"
	apiDoc := kubernetes.K8sApiReference{
		Kind: kind,
		ApiVersion: schema.GroupVersion{
			Group:   "apps",
			Version: "v1",
		},
		OpenapiSchema: a.OpenapiSchema,
	}

	AnalyzerErrorsMetric.DeletePartialMatch(map[string]string{
		"analyzer_name": kind,
	})

	mutatingWebhooks, err := a.Client.GetClient().AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.Background(), v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	validatingWebhooks, err := a.Client.GetClient().AdmissionregistrationV1().ValidatingWebhookConfigurations().List(context.Background(), v1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var preAnalysis = map[string]common.PreAnalysis{}

	for _, webhookConfig := range mutatingWebhooks.Items {
		for _, webhook := range webhookConfig.Webhooks {
			var failures []common.Failure

			svc := webhook.ClientConfig.Service
			pods, err := a.Client.Client.CoreV1().Pods(a.Namespace).List(context.Background(), v1.ListOptions{})
			if err != nil {
				return nil, err
			}
			for _, pod := range pods.Items {
				if pod.Name != svc.Name && pod.Namespace != svc.Namespace && pod.Status.Phase != "Running" {
					doc := apiDoc.GetApiDocV2("spec.webhook")

					failures = append(failures, common.Failure{
						Text: fmt.Sprintf(
							"Mutating Webhook %s is pointing to an inactive receiver pod",
							webhook.Name,
						),
						KubernetesDoc: doc,
						Sensitive: []common.Sensitive{
							{
								Unmasked: webhookConfig.Namespace,
								Masked:   util.MaskString(webhookConfig.Namespace),
							},
							{
								Unmasked: webhook.Name,
								Masked:   util.MaskString(webhook.Name),
							},
						},
					})
				}
				if len(failures) > 0 {
					preAnalysis[fmt.Sprintf("%s/%s", webhookConfig.Namespace, webhook.Name)] = common.PreAnalysis{
						MutatingWebhook: webhookConfig,
						FailureDetails:  failures,
					}
					AnalyzerErrorsMetric.WithLabelValues(kind, webhook.Name, webhookConfig.Namespace).Set(float64(len(failures)))
				}
			}
		}
	}

	for _, webhookConfig := range validatingWebhooks.Items {
		for _, webhook := range webhookConfig.Webhooks {
			var failures []common.Failure

			svc := webhook.ClientConfig.Service
			pods, err := a.Client.Client.CoreV1().Pods(a.Namespace).List(context.Background(), v1.ListOptions{})
			if err != nil {
				return nil, err
			}
			for _, pod := range pods.Items {
				if pod.Name != svc.Name && pod.Namespace != svc.Namespace && pod.Status.Phase != "Running" {
					doc := apiDoc.GetApiDocV2("spec.webhook")

					failures = append(failures, common.Failure{
						Text: fmt.Sprintf(
							"Validating Webhook %s is pointing to an inactive receiver pod",
							webhook.Name,
						),
						KubernetesDoc: doc,
						Sensitive: []common.Sensitive{
							{
								Unmasked: webhookConfig.Namespace,
								Masked:   util.MaskString(webhookConfig.Namespace),
							},
							{
								Unmasked: webhook.Name,
								Masked:   util.MaskString(webhook.Name),
							},
						},
					})
				}
				if len(failures) > 0 {
					preAnalysis[fmt.Sprintf("%s/%s", webhookConfig.Namespace, webhook.Name)] = common.PreAnalysis{
						ValidatingWebhook: webhookConfig,
						FailureDetails:  failures,
					}
					AnalyzerErrorsMetric.WithLabelValues(kind, webhook.Name, webhookConfig.Namespace).Set(float64(len(failures)))
				}
			}
		}
	}
	for key, value := range preAnalysis {
		var currentAnalysis = common.Result{
			Kind:  kind,
			Name:  key,
			Error: value.FailureDetails,
		}

		parent, _ := util.GetParent(a.Client, value.ValidatingWebhook.ObjectMeta)
		currentAnalysis.ParentObject = parent
		a.Results = append(a.Results, currentAnalysis)
	}

	return a.Results, nil
}
