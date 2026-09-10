package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aronk11/correlux/internal/domain/inspection"
)

func TestReverseReferencesAreScopedAndReportIncompleteReads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.Contains(r.URL.Path, "/namespaces/team/") || r.URL.Query().Get("limit") != "200" {
			t.Errorf("unbounded scope: %s", r.URL)
		}
		if strings.HasSuffix(r.URL.Path, "/pods") {
			_, _ = w.Write([]byte(`{"metadata":{"continue":"next"},"items":[{"metadata":{"name":"consumer","namespace":"team"},"spec":{"containers":[{"name":"app","envFrom":[{"secretRef":{"name":"credential"}}]}]}}]}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/jobs") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()
	report, err := debugTestFactory(t, server.URL).ReferencedBy(context.Background(), "staging", inspection.Ref{Kind: "Secret", Name: "credential", Namespace: "team", Resource: "secrets"})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Sections[0].Rows) != 1 || report.Sections[0].Rows[0].Ref.Name != "consumer" {
		t.Fatalf("missing reference: %+v", report)
	}
	if len(report.Gaps) != 2 {
		t.Fatalf("lost partial evidence: %+v", report.Gaps)
	}
}

func TestSelectorlessServiceDoesNotSelectEveryPod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/services/external"):
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"Service","metadata":{"name":"external","namespace":"team"},"spec":{"type":"ExternalName","externalName":"example.test"}}`))
		case strings.Contains(r.URL.Path, "endpointslices"):
			if r.URL.Query().Get("labelSelector") != "kubernetes.io/service-name=external" {
				t.Errorf("wrong selector: %s", r.URL)
			}
			_, _ = w.Write([]byte(`{"apiVersion":"discovery.k8s.io/v1","kind":"EndpointSliceList","items":[]}`))
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	report, err := debugTestFactory(t, server.URL).ServiceRouting(context.Background(), "staging", "team", "external", "", "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(report)
	if !strings.Contains(string(raw), "example.test") || len(report.Gaps) != 0 {
		t.Fatalf("%s", raw)
	}
}

func TestStorageReportRetainsClaimWhenVolumeAccessIsDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/persistentvolumeclaims/data"):
			_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"PersistentVolumeClaim","metadata":{"name":"data","namespace":"team"},"spec":{"volumeName":"pv-data","storageClassName":""},"status":{"phase":"Bound"}}`))
		case strings.HasSuffix(r.URL.Path, "/persistentvolumes/pv-data"):
			http.Error(w, "forbidden", http.StatusForbidden)
		case strings.HasSuffix(r.URL.Path, "/volumeattachments"):
			_, _ = w.Write([]byte(`{"apiVersion":"storage.k8s.io/v1","kind":"VolumeAttachmentList","metadata":{"continue":"next"},"items":[{"metadata":{"name":"match"},"spec":{"nodeName":"worker","source":{"persistentVolumeName":"pv-data"}},"status":{"attached":false,"attachError":{"message":"driver error"}}},{"metadata":{"name":"unrelated"},"spec":{"source":{"persistentVolumeName":"other"}}}]}`))
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer server.Close()
	report, err := debugTestFactory(t, server.URL).StorageContext(context.Background(), "staging", "team", "data")
	if err != nil {
		t.Fatal(err)
	}
	rows := report.Sections[0].Rows
	if len(rows) != 2 || rows[0].Ref.Kind != "PersistentVolumeClaim" || rows[1].Ref.Name != "match" || !strings.Contains(rows[1].Cells[2], "driver error") {
		t.Fatalf("incorrect chain: %+v", rows)
	}
	if len(report.Gaps) != 3 {
		t.Fatalf("missing volume, pagination or driver limit: %v", report.Gaps)
	}
}
