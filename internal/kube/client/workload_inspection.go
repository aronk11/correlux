package client

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/aronk11/correlux/internal/domain/describe"
	"github.com/aronk11/correlux/internal/domain/inspection"
	"github.com/aronk11/correlux/internal/kube/discovery"
)

// WorkloadContext joins rollout revisions, disruption/scaling controls, quotas
// and object Events without treating namespace-wide limits as application usage.
func (f *Factory) WorkloadContext(ctx context.Context, cluster string, ref inspection.Ref, res discovery.Resource) (inspection.Report, error) {
	report := inspection.Report{Title: "Operational context for " + ref.Kind + "/" + ref.Name, Summary: cluster + " / " + ref.Namespace + "; bounded on-demand reads", At: time.Now()}
	obj, err := f.GetObject(ctx, cluster, res, ref.Namespace, ref.Name)
	if err != nil {
		return report, err
	}
	cs, err := f.Clientset(cluster)
	if err != nil {
		return report, err
	}
	for _, section := range describe.Object(ref.Kind, obj.Raw) {
		s := inspection.Section{Title: section.Title, Columns: section.Columns, Empty: section.Empty}
		for _, row := range section.Rows {
			s.Add(row...)
		}
		report.Sections = append(report.Sections, s)
	}
	var workload struct {
		Metadata struct {
			Labels map[string]string `json:"labels"`
		} `json:"metadata"`
		Spec struct {
			NodeName string                 `json:"nodeName"`
			Template corev1.PodTemplateSpec `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(obj.Raw, &workload); err != nil {
		return report, err
	}
	podLabels := workload.Spec.Template.Labels
	if ref.Kind == "Pod" {
		podLabels = workload.Metadata.Labels
	}
	links := inspection.Section{Title: "Dependencies", Columns: []string{"Kind", "Namespace", "Name", "Relationship"}, Empty: "no explicit references"}
	for _, link := range describe.Links(ref.Kind, obj.Raw) {
		links.Link(inspection.Ref{Kind: link.Kind, Name: link.Name, Namespace: link.Namespace, Resource: link.Resource}, link.Kind, link.Namespace, link.Name, link.Detail)
	}
	report.Sections = append(report.Sections, links)
	if ref.Kind == "Deployment" {
		revisions := inspection.Section{Title: "Rollout revisions", Columns: []string{"ReplicaSet", "Revision", "Desired", "Ready", "Images"}, Empty: "no owned ReplicaSets in the loaded page"}
		items, listErr := cs.AppsV1().ReplicaSets(ref.Namespace).List(ctx, metav1.ListOptions{Limit: 200})
		if listErr != nil {
			report.Gaps = append(report.Gaps, "ReplicaSets: "+listErr.Error())
		} else {
			if items.Continue != "" {
				report.Gaps = append(report.Gaps, "ReplicaSets: first 200 only")
			}
			for i := range items.Items {
				rs := &items.Items[i]
				owned := false
				for _, owner := range rs.OwnerReferences {
					if string(owner.UID) == obj.UID && owner.Controller != nil && *owner.Controller {
						owned = true
					}
				}
				if !owned {
					continue
				}
				images := []string{}
				for j := range rs.Spec.Template.Spec.Containers {
					images = append(images, rs.Spec.Template.Spec.Containers[j].Image)
				}
				replicas := "not reported"
				if rs.Spec.Replicas != nil {
					replicas = strconv.Itoa(int(*rs.Spec.Replicas))
				}
				revisions.Link(inspection.Ref{Kind: "ReplicaSet", Name: rs.Name, Namespace: rs.Namespace, Resource: "replicasets.apps"}, rs.Name, rs.Annotations["deployment.kubernetes.io/revision"], replicas, strconv.Itoa(int(rs.Status.ReadyReplicas)), strings.Join(images, ", "))
			}
		}
		report.Sections = append(report.Sections, revisions)
	}
	limits := inspection.Section{Title: "Namespace quotas and defaults", Columns: []string{"Kind", "Name", "Resource", "Used", "Hard / rule"}, Empty: "no quotas or LimitRanges in the loaded pages"}
	quotas, listErr := cs.CoreV1().ResourceQuotas(ref.Namespace).List(ctx, metav1.ListOptions{Limit: 200})
	if listErr != nil {
		report.Gaps = append(report.Gaps, "ResourceQuotas: "+listErr.Error())
	} else {
		if quotas.Continue != "" {
			report.Gaps = append(report.Gaps, "ResourceQuotas: first 200 only")
		}
		for i := range quotas.Items {
			quota := &quotas.Items[i]
			for resource, hard := range quota.Status.Hard {
				used := quota.Status.Used[resource]
				limits.Link(inspection.Ref{Kind: "ResourceQuota", Name: quota.Name, Namespace: quota.Namespace, Resource: "resourcequotas"}, "ResourceQuota", quota.Name, string(resource), used.String(), hard.String())
			}
		}
	}
	ranges, listErr := cs.CoreV1().LimitRanges(ref.Namespace).List(ctx, metav1.ListOptions{Limit: 200})
	if listErr != nil {
		report.Gaps = append(report.Gaps, "LimitRanges: "+listErr.Error())
	} else {
		if ranges.Continue != "" {
			report.Gaps = append(report.Gaps, "LimitRanges: first 200 only")
		}
		for i := range ranges.Items {
			item := &ranges.Items[i]
			limits.Link(inspection.Ref{Kind: "LimitRange", Name: item.Name, Namespace: item.Namespace, Resource: "limitranges"}, "LimitRange", item.Name, "—", "—", fmt.Sprintf("%d default/limit rules; open for details", len(item.Spec.Limits)))
		}
	}
	sort.SliceStable(limits.Rows, func(i, j int) bool {
		return strings.Join(limits.Rows[i].Cells[:3], "\x00") < strings.Join(limits.Rows[j].Cells[:3], "\x00")
	})
	report.Sections = append(report.Sections, limits)
	controllers := inspection.Section{Title: "Scaling and disruption controls", Columns: []string{"Kind", "Name", "Details"}, Empty: "no applicable controls in the loaded pages"}
	budgets, listErr := cs.PolicyV1().PodDisruptionBudgets(ref.Namespace).List(ctx, metav1.ListOptions{Limit: 200})
	if listErr != nil {
		report.Gaps = append(report.Gaps, "PodDisruptionBudgets: "+listErr.Error())
	} else {
		if budgets.Continue != "" {
			report.Gaps = append(report.Gaps, "PodDisruptionBudgets: first 200 only")
		}
		for i := range budgets.Items {
			budget := &budgets.Items[i]
			if budget.Spec.Selector == nil {
				continue
			}
			selector, selectorErr := metav1.LabelSelectorAsSelector(budget.Spec.Selector)
			if selectorErr != nil || !selector.Matches(labels.Set(podLabels)) {
				continue
			}
			controllers.Link(inspection.Ref{Kind: "PodDisruptionBudget", Name: budget.Name, Namespace: budget.Namespace, Resource: "poddisruptionbudgets.policy"}, "PodDisruptionBudget", budget.Name, fmt.Sprintf("healthy %d / required %d; allowed disruptions %d (controller-reported)", budget.Status.CurrentHealthy, budget.Status.DesiredHealthy, budget.Status.DisruptionsAllowed))
		}
	}
	autoscalers, listErr := cs.AutoscalingV2().HorizontalPodAutoscalers(ref.Namespace).List(ctx, metav1.ListOptions{Limit: 200})
	if listErr != nil {
		report.Gaps = append(report.Gaps, "HorizontalPodAutoscalers: "+listErr.Error())
	} else {
		if autoscalers.Continue != "" {
			report.Gaps = append(report.Gaps, "HorizontalPodAutoscalers: first 200 only")
		}
		for i := range autoscalers.Items {
			hpa := &autoscalers.Items[i]
			targetGroup, _, _ := strings.Cut(hpa.Spec.ScaleTargetRef.APIVersion, "/")
			if targetGroup == "v1" {
				targetGroup = ""
			}
			if hpa.Spec.ScaleTargetRef.Name != ref.Name || hpa.Spec.ScaleTargetRef.Kind != ref.Kind || targetGroup != res.Group() {
				continue
			}
			controllers.Link(inspection.Ref{Kind: "HorizontalPodAutoscaler", Name: hpa.Name, Namespace: hpa.Namespace, Resource: "horizontalpodautoscalers.autoscaling"}, "HorizontalPodAutoscaler", hpa.Name, fmt.Sprintf("current %d, desired %d, max %d", hpa.Status.CurrentReplicas, hpa.Status.DesiredReplicas, hpa.Spec.MaxReplicas))
		}
	}
	report.Sections = append(report.Sections, controllers)
	events := inspection.Section{Title: "Events for this object UID", Columns: []string{"Reason", "Count", "Message"}, Empty: "no retained Events in the loaded page"}
	if obj.UID != "" {
		items, eventErr := cs.CoreV1().Events(ref.Namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.uid=" + obj.UID, Limit: 200})
		if eventErr != nil {
			report.Gaps = append(report.Gaps, "Events: "+eventErr.Error())
		} else {
			if items.Continue != "" {
				report.Gaps = append(report.Gaps, "Events: first 200 only")
			}
			for i := range items.Items {
				event := &items.Items[i]
				events.Link(inspection.Ref{Kind: "Event", Name: event.Name, Namespace: event.Namespace, Resource: "events"}, event.Reason, strconv.Itoa(int(event.Count)), event.Message)
			}
		}
	}
	report.Sections = append(report.Sections, events)
	report.Gaps = append(report.Gaps, "Namespace quota usage includes other applications. PDBs govern voluntary disruptions, not general scheduling. Controller state can lag recent changes.")
	return report, nil
}

// StorageContext follows a claim to its volume and recorded node attachments.
func (f *Factory) StorageContext(ctx context.Context, cluster, namespace, name string) (inspection.Report, error) {
	report := inspection.Report{Title: "Storage for PVC/" + name, Summary: cluster + " / " + namespace + "; attachment list capped at 200", At: time.Now()}
	cs, err := f.Clientset(cluster)
	if err != nil {
		return report, err
	}
	claim, err := cs.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return report, err
	}
	section := inspection.Section{Title: "Volume chain", Columns: []string{"Kind", "Name", "State / relationship"}}
	section.Link(inspection.Ref{Kind: "PersistentVolumeClaim", Name: name, Namespace: namespace, Resource: "persistentvolumeclaims"}, "PersistentVolumeClaim", name, string(claim.Status.Phase))
	if claim.Spec.StorageClassName != nil && *claim.Spec.StorageClassName != "" {
		section.Link(inspection.Ref{Kind: "StorageClass", Name: *claim.Spec.StorageClassName, Resource: "storageclasses.storage.k8s.io"}, "StorageClass", *claim.Spec.StorageClassName, "provisioning class")
	}
	if claim.Spec.VolumeName == "" {
		report.Sections = []inspection.Section{section}
		report.Gaps = append(report.Gaps, "No bound volume is reported. Inspect the claim's Events and storage class for provisioning evidence.")
		return report, nil
	}
	volume, volumeErr := cs.CoreV1().PersistentVolumes().Get(ctx, claim.Spec.VolumeName, metav1.GetOptions{})
	if volumeErr != nil {
		report.Gaps = append(report.Gaps, "PersistentVolume: "+volumeErr.Error())
	} else {
		section.Link(inspection.Ref{Kind: "PersistentVolume", Name: volume.Name, Resource: "persistentvolumes"}, "PersistentVolume", volume.Name, string(volume.Status.Phase)+"; reclaim "+string(volume.Spec.PersistentVolumeReclaimPolicy))
	}
	attachments, listErr := cs.StorageV1().VolumeAttachments().List(ctx, metav1.ListOptions{Limit: 200})
	if listErr != nil {
		report.Gaps = append(report.Gaps, "VolumeAttachments: "+listErr.Error())
	} else {
		if attachments.Continue != "" {
			report.Gaps = append(report.Gaps, "VolumeAttachments: additional objects were not scanned")
		}
		for i := range attachments.Items {
			attachment := &attachments.Items[i]
			if attachment.Spec.Source.PersistentVolumeName == nil || *attachment.Spec.Source.PersistentVolumeName != claim.Spec.VolumeName {
				continue
			}
			detail := fmt.Sprintf("attached=%t; node %s", attachment.Status.Attached, attachment.Spec.NodeName)
			if attachment.Status.AttachError != nil {
				detail += "; " + attachment.Status.AttachError.Message
			}
			if attachment.Status.DetachError != nil {
				detail += "; " + attachment.Status.DetachError.Message
			}
			section.Link(inspection.Ref{Kind: "VolumeAttachment", Name: attachment.Name, Resource: "volumeattachments.storage.k8s.io"}, "VolumeAttachment", attachment.Name, detail)
		}
	}
	report.Sections = []inspection.Section{section}
	report.Gaps = append(report.Gaps, "Some storage drivers do not use VolumeAttachment objects. Absence alone does not indicate failure.")
	return report, nil
}
