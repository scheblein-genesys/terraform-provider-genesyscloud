package tfexporter

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	gcloud "terraform-provider-genesyscloud/genesyscloud"
	architectFlow "terraform-provider-genesyscloud/genesyscloud/architect_flow"
	userPromptResource "terraform-provider-genesyscloud/genesyscloud/architect_user_prompt"
	authDivision "terraform-provider-genesyscloud/genesyscloud/auth_division"
	obContactList "terraform-provider-genesyscloud/genesyscloud/outbound_contact_list"
	"terraform-provider-genesyscloud/genesyscloud/platform"
	"terraform-provider-genesyscloud/genesyscloud/provider"
	resourceExporter "terraform-provider-genesyscloud/genesyscloud/resource_exporter"
	routingQueue "terraform-provider-genesyscloud/genesyscloud/routing_queue"
	routingWrapupcode "terraform-provider-genesyscloud/genesyscloud/routing_wrapupcode"
	telephonyProvidersEdgesSite "terraform-provider-genesyscloud/genesyscloud/telephony_providers_edges_site"
	tbs "terraform-provider-genesyscloud/genesyscloud/telephony_providers_edges_trunkbasesettings"
	"terraform-provider-genesyscloud/genesyscloud/user"
	"terraform-provider-genesyscloud/genesyscloud/util"
	"testing"
	"time"

	"github.com/mypurecloud/platform-client-sdk-go/v154/platformclientv2"

	"terraform-provider-genesyscloud/genesyscloud/util/testrunner"

	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"

	"github.com/google/uuid"

	userPrompt "terraform-provider-genesyscloud/genesyscloud/architect_user_prompt"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"gonum.org/v1/gonum/graph/simple"
	"gonum.org/v1/gonum/graph/topo"
)

type UserExport struct {
	Email                 string `json:"email"`
	ExportedLabel         string `json:"name"`
	State                 string `json:"state"`
	OriginalResourceLabel string ``
}

type QueueExport struct {
	AcwTimeoutMs          int    `json:"acw_timeout_ms"`
	Description           string `json:"description"`
	ExportedLabel         string `json:"name"`
	OriginalResourceLabel string ``
}

type WrapupcodeExport struct {
	Name                  string `json:"name"`
	OriginalResourceLabel string ``
}

var (
	mccMutex sync.RWMutex
)

func init() {
	mccMutex = sync.RWMutex{}
}

// TestAccResourceTfExportIncludeFilterResourcesByRegEx will create 4 queues (three ending with -prod and then one watching with -test).  The
// The code will use a regex to include all queues that have a label that match a regular expression.  (e.g. -prod).  The test checks to see if any -test
// queues are exported.
func TestAccResourceTfExportIncludeFilterResourcesByRegEx(t *testing.T) {
	var (
		exportTestDir       = testrunner.GetTestTempPath(".terraformregex" + uuid.NewString())
		exportResourceLabel = "test-export3"
		uniquePostfix       = randString(7)
		queueResources      = []QueueExport{
			{OriginalResourceLabel: "test-queue-prod-1", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod-" + uniquePostfix, Description: "This is a test prod queue 1", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-prod-2", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod-" + uniquePostfix, Description: "This is a test prod queue 2", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-prod-3", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod-" + uniquePostfix, Description: "This is a test prod queue 3", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-test-4", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod-" + uniquePostfix, Description: "This is a test prod queue 4", AcwTimeoutMs: 200000},
		}
	)
	defer os.RemoveAll(exportTestDir)

	queueResourceDef := buildQueueResources(queueResources)
	baseConfig := queueResourceDef
	configWithExporter := baseConfig + generateTfExportByIncludeFilterResources(
		exportResourceLabel,
		exportTestDir,
		util.TrueValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue::.*-prod"),
		},
		strconv.Quote("json"),
		util.FalseValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue." + queueResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[2].OriginalResourceLabel),
		},
	)

	sanitizer := resourceExporter.NewSanitizerProvider()

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				// Generate a queue
				Config: baseConfig,
			},
			{
				// Now, export it
				Config: configWithExporter,
				Check: resource.ComposeTestCheckFunc(
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[0].ExportedLabel), queueResources[0]),
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[1].ExportedLabel), queueResources[1]),
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[2].ExportedLabel), queueResources[2]),
					testQueueExportMatchesRegEx(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", "-prod"), //We should not find any "test" queues here because we only wanted to include queues that ended with a -prod
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

// TestAccResourceTfExportIncludeFilterResourcesByRegExAndSanitizedLabels will create 3 queues (twoc with foo bar, one to be excluded).
// The test ensures that resources can be exported directly by their actual label or their sanitized label.
func TestAccResourceTfExportIncludeFilterResourcesByRegExAndSanitizedLabels(t *testing.T) {
	var (
		exportTestDir       = testrunner.GetTestTempPath(".terraformregex" + uuid.NewString())
		exportResourceLabel = "test-export3_1"
		uniquePostfix       = randString(7)
		queueResources      = []QueueExport{
			{OriginalResourceLabel: "test-queue-test-1", ExportedLabel: "include filter test - exclude me" + uuid.NewString() + uniquePostfix, Description: "This is an excluded bar test resource", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-test-2", ExportedLabel: "include filter test - foo - bar me" + uuid.NewString() + uniquePostfix, Description: "This is a foo bar test resource", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-test-3", ExportedLabel: "include filter test - fu - barre you" + uuid.NewString() + uniquePostfix, Description: "This is a foo bar test resource", AcwTimeoutMs: 200000},
		}
	)
	defer os.RemoveAll(exportTestDir)

	queueResourceDef := buildQueueResources(queueResources)

	baseConfig := queueResourceDef
	configWithExporter := baseConfig + generateTfExportByIncludeFilterResources(
		exportResourceLabel,
		exportTestDir,
		util.TrueValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue::include filter test - foo - bar me"),   // Unsanitized Label Resource
			strconv.Quote("genesyscloud_routing_queue::include_filter_test_-_fu_-_barre_you"), // Sanitized Label Resource
		},
		strconv.Quote("json"),
		util.FalseValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue." + queueResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[2].OriginalResourceLabel),
		},
	)

	sanitizer := resourceExporter.NewSanitizerProvider()

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				// Generate a queue
				Config: baseConfig,
			},
			{
				// Export the queue
				Config: configWithExporter,
				Check: resource.ComposeTestCheckFunc(
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[1].ExportedLabel), queueResources[1]),
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[2].ExportedLabel), queueResources[2]),
					testQueueExportExcludesRegEx(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", ".*exclude me.*"), //We should not find any "test" queues here because we only wanted to include queues that ended with a -prod
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

// TestAccResourceTfExportIncludeFilterResourcesByRegExExclusiveToResource
// will create two queues (one with a -prod suffix and one with a -test suffix)
// and two wrap up codes (one with a -prod suffix and one with a -test suffix)
// The code will use a regex to include queue resources with -prod suffix
// but wrap up code resources will not have any regex filter.
// eg. queue ending with prod should be exported but all wrap up codes should be
// exported as well
func TestAccResourceTfExportIncludeFilterResourcesByRegExExclusiveToResource(t *testing.T) {
	var (
		exportTestDir       = testrunner.GetTestTempPath(".terraformInclude" + uuid.NewString())
		exportResourceLabel = "test-export4"
		uniquePostfix       = randString(7)

		queueResources = []QueueExport{
			{OriginalResourceLabel: "test-queue-prod", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod-" + uniquePostfix, Description: "This is the prod queue", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-test", ExportedLabel: "test-queue-" + uuid.NewString() + "-test-" + uniquePostfix, Description: "This is the test queue", AcwTimeoutMs: 200000},
		}

		wrapupCodeResources = []WrapupcodeExport{
			{OriginalResourceLabel: "test-wrapupcode-prod", Name: "test-wrapupcode-" + uuid.NewString() + "-prod-" + uniquePostfix},
			{OriginalResourceLabel: "test-wrapupcode-test", Name: "test-wrapupcode-" + uuid.NewString() + "-test-" + uniquePostfix},
		}
		divResourceLabel = "test-division"
		description      = "Terraform wrapup code description"
		divName          = "terraform-" + uuid.NewString()
	)
	defer os.RemoveAll(exportTestDir)

	queueResourceDef := buildQueueResources(queueResources)
	wrapupcodeResourceDef := buildWrapupcodeResources(wrapupCodeResources, "genesyscloud_auth_division."+divResourceLabel+".id", description)
	baseConfig := queueResourceDef + authDivision.GenerateAuthDivisionBasic(divResourceLabel, divName) + wrapupcodeResourceDef
	configWithExporter := baseConfig + generateTfExportByIncludeFilterResources(
		exportResourceLabel,
		exportTestDir,
		util.TrueValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue::.*-prod"),
			strconv.Quote("genesyscloud_routing_wrapupcode"),
		},
		strconv.Quote("json"),
		util.FalseValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue." + queueResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[1].OriginalResourceLabel),
		},
	)

	sanitizer := resourceExporter.NewSanitizerProvider()

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				// Generate a queue
				Config: baseConfig,
			},
			{
				// Export the queue
				Config: configWithExporter,
				Check: resource.ComposeTestCheckFunc(
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[0].ExportedLabel), queueResources[0]),
					testWrapupcodeExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_wrapupcode", sanitizer.S.SanitizeResourceBlockLabel(wrapupCodeResources[0].Name), wrapupCodeResources[0]),
					testWrapupcodeExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_wrapupcode", sanitizer.S.SanitizeResourceBlockLabel(wrapupCodeResources[1].Name), wrapupCodeResources[1]),
					testQueueExportMatchesRegEx(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", ".*-prod"), //We should not find any "test" queues here because we only wanted to include queues that ended with a -prod
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

// TestAccResourceTfExportExcludeFilterResourcesByRegExExclusiveToResource will exclude any test resources that match a
// regular expression provided for the resource. In this test we expect queues ending with -test or -dev to be excluded.
// Wrap up codes will be created with -prod, -dev and -test suffixes but they should not be affected by the regex filter
// for the queue and should all be exported
func TestAccResourceTfExportExcludeFilterResourcesByRegExExclusiveToResource(t *testing.T) {
	t.Skip("Skipping until DEVTOOLING-1096 is addressed.")
	var (
		exportTestDir       = testrunner.GetTestTempPath(".terraformExclude" + uuid.NewString())
		exportResourceLabel = "test-export6"
		uniquePostfix       = randString(7)
		queueResources      = []QueueExport{
			{OriginalResourceLabel: "test-queue-prod", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod-" + uniquePostfix, Description: "This is a test prod queue", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-test", ExportedLabel: "test-queue-" + uuid.NewString() + "-test-" + uniquePostfix, Description: "This is a test queue", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-dev", ExportedLabel: "test-queue-" + uuid.NewString() + "-dev-" + uniquePostfix, Description: "This is a dev queue", AcwTimeoutMs: 200000},
		}

		wrapupCodeResources = []WrapupcodeExport{
			{OriginalResourceLabel: "test-wrapupcode-prod", Name: "test-wrapupcode-" + uuid.NewString() + "-prod-" + uniquePostfix},
			{OriginalResourceLabel: "test-wrapupcode-test", Name: "test-wrapupcode-" + uuid.NewString() + "-test-" + uniquePostfix},
			{OriginalResourceLabel: "test-wrapupcode-dev", Name: "test-wrapupcode-" + uuid.NewString() + "-dev-" + uniquePostfix},
		}
		divResourceLabel = "test-division"
		divName          = "terraform-" + uuid.NewString()
		description      = "Terraform wrapup code description"
	)
	defer os.RemoveAll(exportTestDir)

	queueResourceDef := buildQueueResources(queueResources)
	wrapupcodeResourceDef := buildWrapupcodeResources(wrapupCodeResources, "genesyscloud_auth_division."+divResourceLabel+".id", description)
	baseConfig := queueResourceDef + authDivision.GenerateAuthDivisionBasic(divResourceLabel, divName) + wrapupcodeResourceDef
	configWithExporter := baseConfig + generateTfExportByExcludeFilterResources(
		exportResourceLabel,
		exportTestDir,
		util.TrueValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue::.*-(dev|test)"),
			strconv.Quote("genesyscloud_outbound_ruleset"),
			strconv.Quote("genesyscloud_user"),
			strconv.Quote("genesyscloud_user_roles"),
			strconv.Quote("genesyscloud_flow"),
		},
		strconv.Quote("json"),
		util.FalseValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue." + queueResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[2].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[2].OriginalResourceLabel),
		},
	)

	sanitizer := resourceExporter.NewSanitizerProvider()
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				// Generate a queue with wrapup codes
				Config: baseConfig,
			},
			{
				// Generate a queue with wrapup codes and exporter
				Config: configWithExporter,
				Check: resource.ComposeTestCheckFunc(
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[0].ExportedLabel), queueResources[0]),
					testWrapupcodeExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_wrapupcode", sanitizer.S.SanitizeResourceBlockLabel(wrapupCodeResources[0].Name), wrapupCodeResources[0]),
					testWrapupcodeExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_wrapupcode", sanitizer.S.SanitizeResourceBlockLabel(wrapupCodeResources[1].Name), wrapupCodeResources[1]),
					testWrapupcodeExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_wrapupcode", sanitizer.S.SanitizeResourceBlockLabel(wrapupCodeResources[2].Name), wrapupCodeResources[2]),
					testQueueExportExcludesRegEx(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", ".*-(dev|test)"),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

// TestAccResourceTfExportSplitFilesAsJSON will create 2 queues, 2 wrap up codes, and 2 users.
// The exporter will be run in split mode so 3 resource tf.jsons should be created as well as a provider.tf.json
func TestAccResourceTfExportSplitFilesAsJSON(t *testing.T) {
	var (
		exportTestDir       = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		exportResourceLabel = "test-export-split"
		uniquePostfix       = randString(7)
		expectedFilesPath   = []string{
			filepath.Join(exportTestDir, "genesyscloud_routing_queue.tf.json"),
			filepath.Join(exportTestDir, "genesyscloud_user.tf.json"),
			filepath.Join(exportTestDir, "genesyscloud_routing_wrapupcode.tf.json"),
			filepath.Join(exportTestDir, "provider.tf.json"),
		}

		queueResources = []QueueExport{
			{OriginalResourceLabel: "test-queue-1", ExportedLabel: "test-queue-1-" + uuid.NewString() + uniquePostfix, Description: "This is a test queue", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-2", ExportedLabel: "test-queue-1-" + uuid.NewString() + uniquePostfix, Description: "This is a test queue too", AcwTimeoutMs: 200000},
		}

		userResources = []UserExport{
			{OriginalResourceLabel: "test-user-1", ExportedLabel: "test-user-1" + uuid.NewString() + uniquePostfix, Email: "test-user-1" + uuid.NewString() + "@test.com" + uniquePostfix, State: "active"},
			{OriginalResourceLabel: "test-user-2", ExportedLabel: "test-user-2" + uuid.NewString() + uniquePostfix, Email: "test-user-2" + uuid.NewString() + "@test.com" + uniquePostfix, State: "active"},
		}

		wrapupCodeResources = []WrapupcodeExport{
			{OriginalResourceLabel: "test-wrapupcode-1", Name: "test-wrapupcode-1-" + uuid.NewString() + uniquePostfix},
			{OriginalResourceLabel: "test-wrapupcode-2", Name: "test-wrapupcode-2-" + uuid.NewString() + uniquePostfix},
		}

		divResourceLabel = "test-division"
		divName          = "terraform-" + uuid.NewString()
		description      = "Terraform wrapup code description"
	)
	defer os.RemoveAll(exportTestDir)

	queueResourceDef := buildQueueResources(queueResources)
	userResourcesDef := buildUserResources(userResources)
	wrapupcodeResourceDef := buildWrapupcodeResources(wrapupCodeResources, "genesyscloud_auth_division."+divResourceLabel+".id", description)
	baseConfig := queueResourceDef + authDivision.GenerateAuthDivisionBasic(divResourceLabel, divName) + wrapupcodeResourceDef + userResourcesDef
	configWithExporter := baseConfig + generateTfExportByIncludeFilterResources(
		exportResourceLabel,
		exportTestDir,
		util.TrueValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue::" + uniquePostfix + "$"),
			strconv.Quote("genesyscloud_user::" + uniquePostfix + "$"),
			strconv.Quote("genesyscloud_routing_wrapupcode::" + uniquePostfix + "$"),
		},
		strconv.Quote("json"),
		util.TrueValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue." + queueResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_user." + userResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_user." + userResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[1].OriginalResourceLabel),
		},
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: baseConfig,
			},
			{
				Config: configWithExporter,
				Check: resource.ComposeTestCheckFunc(
					validateFileCreated(expectedFilesPath[0]),
					validateFileCreated(expectedFilesPath[1]),
					validateFileCreated(expectedFilesPath[2]),
					validateFileCreated(expectedFilesPath[3]),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

// TestAccResourceTfExportExcludeFilterResourcesByRegExExclusiveToResourceAndSanitizedLabels will exclude any test resources that match a
// regular expression provided for the resource. In this test we check against both sanitized and unsanitized labels.
func TestAccResourceTfExportExcludeFilterResourcesByRegExExclusiveToResourceAndSanitizedLabels(t *testing.T) {
	t.Skip("Skipping until DEVTOOLING-1120 is resolved")
	var (
		exportTestDir       = testrunner.GetTestTempPath(".terraformExclude" + uuid.NewString())
		exportResourceLabel = "test-export6_1"
		uniquePostfix       = randString(7)

		queueResources = []QueueExport{
			{OriginalResourceLabel: "test-queue-test-1", ExportedLabel: "exclude filter - exclude me" + uuid.NewString() + uniquePostfix, Description: "This is an excluded bar test resource", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-test-2", ExportedLabel: "exclude filter - foo - bar me" + uuid.NewString() + uniquePostfix, Description: "This is a foo bar test resource", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-test-3", ExportedLabel: "exclude filter - fu - barre you" + uuid.NewString() + uniquePostfix, Description: "This is a foo bar test resource", AcwTimeoutMs: 200000},
		}

		wrapupCodeResources = []WrapupcodeExport{
			{OriginalResourceLabel: "test-wrapupcode-prod", Name: "exclude me" + uuid.NewString() + uniquePostfix},
			{OriginalResourceLabel: "test-wrapupcode-test", Name: "foo + bar me" + uuid.NewString() + uniquePostfix},
			{OriginalResourceLabel: "test-wrapupcode-dev", Name: "fu - barre you" + uuid.NewString() + uniquePostfix},
		}
		divResourceLabel = "test-division"
		divName          = "terraform-" + uuid.NewString()
		description      = "Terraform wrapup code description"
	)
	cleanupFunc := func() {
		if provider.SdkClientPool != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = provider.SdkClientPool.Close(ctx)
		}
		if err := os.RemoveAll(exportTestDir); err != nil {
			t.Logf("Error while cleaning up %v", err)
		}
	}
	t.Cleanup(cleanupFunc)

	queueResourceDef := buildQueueResources(queueResources)
	wrapupcodeResourceDef := buildWrapupcodeResources(wrapupCodeResources, "genesyscloud_auth_division."+divResourceLabel+".id", description)
	baseConfig := queueResourceDef + authDivision.GenerateAuthDivisionBasic(divResourceLabel, divName) + wrapupcodeResourceDef
	configWithExporter := baseConfig + generateTfExportByExcludeFilterResources(
		exportResourceLabel,
		exportTestDir,
		util.TrueValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue::exclude filter - foo - bar me"),
			strconv.Quote("genesyscloud_routing_queue::exclude_filter_-_fu_-_barre_you"),
			strconv.Quote("genesyscloud_outbound_ruleset"),
			strconv.Quote("genesyscloud_user"),
			strconv.Quote("genesyscloud_user_roles"),
			strconv.Quote("genesyscloud_flow"),
		},
		strconv.Quote("json"),
		util.FalseValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue." + queueResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[2].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[2].OriginalResourceLabel),
		},
	)

	sanitizer := resourceExporter.NewSanitizerProvider()
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				// Generate a queue
				Config: baseConfig,
			},
			{
				// Now export it
				Config: configWithExporter,
				Check: resource.ComposeTestCheckFunc(
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[0].ExportedLabel), queueResources[0]),
					testWrapupcodeExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_wrapupcode", sanitizer.S.SanitizeResourceBlockLabel(wrapupCodeResources[0].Name), wrapupCodeResources[0]),
					testWrapupcodeExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_wrapupcode", sanitizer.S.SanitizeResourceBlockLabel(wrapupCodeResources[1].Name), wrapupCodeResources[1]),
					testWrapupcodeExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_wrapupcode", sanitizer.S.SanitizeResourceBlockLabel(wrapupCodeResources[2].Name), wrapupCodeResources[2]),
					testQueueExportExcludesRegEx(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", "(foo|fu)"),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

// TestAccResourceTfExportForCompress does a basic test check to make sure the compressed file is created.
func TestAccResourceTfExportForCompress(t *testing.T) {
	var (
		exportTestDir        = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		exportResourceLabel1 = "test-export1"
		zipFileName          = filepath.Join(exportTestDir, "..", "archive_genesyscloud_tf_export*")
		divResourceLabel     = "test-division"
		divName              = "terraform-" + uuid.NewString()
	)

	baseConfig := authDivision.GenerateAuthDivisionBasic(divResourceLabel, divName)
	defer os.RemoveAll(exportTestDir)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			// Create basic object
			{
				Config: baseConfig,
			},
			{
				// Run export without state file
				Config: baseConfig + generateTfExportResourceForCompress(
					exportResourceLabel1,
					exportTestDir,
					util.TrueValue,
					util.TrueValue,
					[]string{
						strconv.Quote(authDivision.ResourceType),
					},
					"",
				),
				Check: resource.ComposeTestCheckFunc(
					validateCompressedCreated(zipFileName),
					validateCompressedFile(zipFileName),
				),
			},
		},
		CheckDestroy: deleteTestCompressedZip(exportTestDir, zipFileName),
	})
}

// TestAccResourceTfExport does a basic test check to make sure the export file is created.
func TestAccResourceTfExport(t *testing.T) {
	var (
		exportTestDir        = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		exportResourceLabel1 = "test-export1"
		configPath           = filepath.Join(exportTestDir, defaultTfJSONFile)
		statePath            = filepath.Join(exportTestDir, defaultTfStateFile)
	)

	defer os.RemoveAll(exportTestDir)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				// Run export without state file
				Config: generateTfExportResource(
					exportResourceLabel1,
					exportTestDir,
					util.TrueValue,
					"",
				),
				Check: resource.ComposeTestCheckFunc(
					validateFileCreated(configPath),
					validateConfigFile(configPath),
				),
			},
			{
				// Run export with state file and excluded attribute
				Config: generateTfExportResource(
					exportResourceLabel1,
					exportTestDir,
					util.TrueValue,
					strconv.Quote("genesyscloud_auth_role.permission_policies.conditions"),
				),
				Check: resource.ComposeTestCheckFunc(
					validateFileCreated(configPath),
					validateConfigFile(configPath),
					validateFileCreated(statePath),
				),
			},
			{
				// Run export with state file and excluded attribute with regex
				Config: generateTfExportResourceMin(
					exportResourceLabel1,
					exportTestDir,
					util.TrueValue,
					strconv.Quote("g*.name"),
				),
				Check: resource.ComposeTestCheckFunc(
					validateFileCreated(configPath),
					validateConfigFile(configPath),
					validateFileCreated(statePath),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

func TestAccResourceTfExportByLabel(t *testing.T) {
	var (
		exportTestDir        = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		exportResourceLabel1 = "test-export1"

		userResourceLabel1 = "test-user1"
		userEmail1         = "terraform-" + uuid.NewString() + "@example.com"
		userName1          = "John Data-" + uuid.NewString()

		userResourceLabel2 = "test-user2"
		userEmail2         = "terraform-" + uuid.NewString() + "@example.com"
		userName2          = "John Data-" + uuid.NewString()

		queueResourceLabel = "test-queue"
		queueName          = "Terraform Test Queue-" + uuid.NewString()
		queueDesc          = "This is a test"
		queueAcwTimeout    = 200000
	)

	defer os.RemoveAll(exportTestDir)

	testUser1 := &UserExport{
		ExportedLabel: userName1,
		Email:         userEmail1,
		State:         "active",
	}

	testUser2 := &UserExport{
		ExportedLabel: userName2,
		Email:         userEmail2,
		State:         "active",
	}

	testQueue := &QueueExport{
		ExportedLabel: queueName,
		Description:   queueDesc,
		AcwTimeoutMs:  queueAcwTimeout,
	}

	sanitizer := resourceExporter.NewSanitizerProvider()

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				// Generate a user
				Config: user.GenerateBasicUserResource(
					userResourceLabel1,
					userEmail1,
					userName1,
				),
			},
			{
				// Now export it
				Config: user.GenerateBasicUserResource(
					userResourceLabel1,
					userEmail1,
					userName1,
				) + generateTfExportByFilter(
					exportResourceLabel1,
					exportTestDir,
					util.TrueValue,
					[]string{strconv.Quote("genesyscloud_user::" + userEmail1)},
					"",
					strconv.Quote("json"),
					util.FalseValue,
					[]string{strconv.Quote("genesyscloud_user." + userResourceLabel1)},
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("genesyscloud_tf_export."+exportResourceLabel1,
						"resource_types.0", "genesyscloud_user::"+userEmail1),
					testUserExport(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_user", sanitizer.S.SanitizeResourceBlockLabel(userEmail1), testUser1),
				),
			},
			{
				// Generate a queue as well and export it
				Config: user.GenerateBasicUserResource(
					userResourceLabel1,
					userEmail1,
					userName1,
				) + routingQueue.GenerateRoutingQueueResource(
					queueResourceLabel,
					queueName,
					queueDesc,
					util.NullValue,                     // MANDATORY_TIMEOUT
					fmt.Sprintf("%v", queueAcwTimeout), // acw_timeout
					util.NullValue,                     // ALL
					util.NullValue,                     // auto_answer_only true
					util.NullValue,                     // No calling party name
					util.NullValue,                     // No calling party number
					util.NullValue,                     // enable_audio_monitoring false
					util.NullValue,                     // enable_manual_assignment false
					util.FalseValue,                    // suppressCall_record_false
					util.NullValue,                     // enable_transcription false
					strconv.Quote("TimestampAndPriority"),
					util.NullValue,
					util.NullValue,
				) + generateTfExportByFilter(
					exportResourceLabel1,
					exportTestDir,
					util.TrueValue,
					[]string{
						strconv.Quote("genesyscloud_user::" + userEmail1),
						strconv.Quote("genesyscloud_routing_queue::" + queueName),
					},
					"",
					strconv.Quote("json"),
					util.FalseValue,
					[]string{strconv.Quote("genesyscloud_user." + userResourceLabel1)},
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("genesyscloud_tf_export."+exportResourceLabel1,
						"resource_types.0", "genesyscloud_user::"+userEmail1),
					resource.TestCheckResourceAttr("genesyscloud_tf_export."+exportResourceLabel1,
						"resource_types.1", "genesyscloud_routing_queue::"+queueName),
					testUserExport(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_user", sanitizer.S.SanitizeResourceBlockLabel(userEmail1), testUser1),
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueName), *testQueue),
				),
			},
			{
				// Export all trunk base settings as well
				Config: user.GenerateBasicUserResource(
					userResourceLabel1,
					userEmail1,
					userName1,
				) + routingQueue.GenerateRoutingQueueResource(
					queueResourceLabel,
					queueName,
					queueDesc,
					util.NullValue,                     // MANDATORY_TIMEOUT
					fmt.Sprintf("%v", queueAcwTimeout), // acw_timeout
					util.NullValue,                     // ALL
					util.NullValue,                     // auto_answer_only true
					util.NullValue,                     // No calling party name
					util.NullValue,                     // No calling party number
					util.NullValue,                     // enable_audio_monitoring false
					util.NullValue,                     // enable_manual_assignment false
					util.FalseValue,                    // suppressCall_record_false
					util.NullValue,                     // enable_transcription false
					strconv.Quote("TimestampAndPriority"),
					util.NullValue,
					util.NullValue,
				) + generateTfExportByFilter(
					exportResourceLabel1,
					exportTestDir,
					util.TrueValue,
					[]string{
						strconv.Quote("genesyscloud_user::" + userEmail1),
						strconv.Quote("genesyscloud_routing_queue::" + queueName),
						strconv.Quote("genesyscloud_telephony_providers_edges_trunkbasesettings"),
					},
					"",
					strconv.Quote("json"),
					util.FalseValue,
					[]string{
						strconv.Quote("genesyscloud_routing_queue." + queueResourceLabel),
						strconv.Quote("genesyscloud_user." + userResourceLabel1)},
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"genesyscloud_tf_export."+exportResourceLabel1, "resource_types.0",
						"genesyscloud_user::"+userEmail1),
					resource.TestCheckResourceAttr(
						"genesyscloud_tf_export."+exportResourceLabel1, "resource_types.1",
						"genesyscloud_routing_queue::"+queueName),
					resource.TestCheckResourceAttr(
						"genesyscloud_tf_export."+exportResourceLabel1, "resource_types.2",
						"genesyscloud_telephony_providers_edges_trunkbasesettings"),
					testUserExport(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_user", sanitizer.S.SanitizeResourceBlockLabel(userEmail1), testUser1),
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueName), *testQueue),
					testTrunkBaseSettingsExport(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_telephony_providers_edges_trunkbasesettings"),
				),
			},
			{
				// Export all trunk base settings as well
				Config: user.GenerateBasicUserResource(
					userResourceLabel1,
					userEmail1,
					userName1,
				) + user.GenerateBasicUserResource(
					userResourceLabel2,
					userEmail2,
					userName2,
				) + routingQueue.GenerateRoutingQueueResource(
					queueResourceLabel,
					queueName,
					queueDesc,
					util.NullValue,                     // MANDATORY_TIMEOUT
					fmt.Sprintf("%v", queueAcwTimeout), // acw_timeout
					util.NullValue,                     // ALL
					util.NullValue,                     // auto_answer_only true
					util.NullValue,                     // No calling party name
					util.NullValue,                     // No calling party number
					util.NullValue,                     // enable_audio_monitoring false
					util.NullValue,                     // enable_manual_assignment false
					util.FalseValue,                    // suppressCall_record_false
					util.NullValue,                     // enable_transcription false
					strconv.Quote("TimestampAndPriority"),
					util.NullValue,
					util.NullValue,
				) + generateTfExportByFilter(
					exportResourceLabel1,
					exportTestDir,
					util.TrueValue,
					[]string{
						strconv.Quote("genesyscloud_user::" + userEmail1),
						strconv.Quote("genesyscloud_user::" + userEmail2),
						strconv.Quote("genesyscloud_routing_queue::" + queueName),
						strconv.Quote("genesyscloud_telephony_providers_edges_trunkbasesettings"),
					},
					"",
					strconv.Quote("json"),
					util.FalseValue,
					[]string{
						strconv.Quote("genesyscloud_routing_queue." + queueResourceLabel),
						strconv.Quote("genesyscloud_user." + userResourceLabel1),
						strconv.Quote("genesyscloud_user." + userResourceLabel2)},
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(
						"genesyscloud_tf_export."+exportResourceLabel1, "resource_types.0",
						"genesyscloud_user::"+userEmail1),
					resource.TestCheckResourceAttr(
						"genesyscloud_tf_export."+exportResourceLabel1, "resource_types.1",
						"genesyscloud_user::"+userEmail2),
					resource.TestCheckResourceAttr(
						"genesyscloud_tf_export."+exportResourceLabel1, "resource_types.2",
						"genesyscloud_routing_queue::"+queueName),
					resource.TestCheckResourceAttr(
						"genesyscloud_tf_export."+exportResourceLabel1, "resource_types.3",
						"genesyscloud_telephony_providers_edges_trunkbasesettings"),
					testUserExport(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_user", sanitizer.S.SanitizeResourceBlockLabel(userEmail1), testUser1),
					testUserExport(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_user", sanitizer.S.SanitizeResourceBlockLabel(userEmail2), testUser2),
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueName), *testQueue),
					testTrunkBaseSettingsExport(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_telephony_providers_edges_trunkbasesettings"),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

func TestAccResourceTfExportIncludeFilterResourcesByType(t *testing.T) {
	var (
		exportTestDir       = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		exportResourceLabel = "test-export2"
		uniquePostfix       = randString(7)
	)

	queueResources := []QueueExport{
		{OriginalResourceLabel: "test-queue-prod-1", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod" + uniquePostfix, Description: "This is a test prod queue 1", AcwTimeoutMs: 200000},
		{OriginalResourceLabel: "test-queue-prod-2", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod" + uniquePostfix, Description: "This is a test prod queue 2", AcwTimeoutMs: 200000},
		{OriginalResourceLabel: "test-queue-prod-3", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod" + uniquePostfix, Description: "This is a test prod queue 3", AcwTimeoutMs: 200000},
		{OriginalResourceLabel: "test-queue-test-1", ExportedLabel: "test-queue-" + uuid.NewString() + "-test" + uniquePostfix, Description: "This is a test prod queue 4", AcwTimeoutMs: 200000},
	}

	defer os.RemoveAll(exportTestDir)

	queueResourceDef := buildQueueResources(queueResources)
	baseConfig := queueResourceDef
	configWithExporter := baseConfig + generateTfExportByIncludeFilterResources(
		exportResourceLabel,
		exportTestDir,
		util.TrueValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue"),
		},
		strconv.Quote("json"),
		util.FalseValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue." + queueResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[2].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[3].OriginalResourceLabel),
		},
	)

	sanitizer := resourceExporter.NewSanitizerProvider()
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				// Generate a queues
				Config: baseConfig,
			},
			{
				// Now export them
				Config: configWithExporter,
				Check: resource.ComposeTestCheckFunc(
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[0].ExportedLabel), queueResources[0]),
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[1].ExportedLabel), queueResources[1]),
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[2].ExportedLabel), queueResources[2]),
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[3].ExportedLabel), queueResources[3]),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

// TestAccResourceTfExportExcludeFilterResourcesByRegEx will exclude any test resources that match a regular expression provided.  In our test case we exclude
// all routing queues that have a regex with -(dev|test)$ in it.  We then check to see if there are any prod queues present.
func TestAccResourceTfExportExcludeFilterResourcesByRegEx(t *testing.T) {
	var (
		exportTestDir       = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		exportResourceLabel = "test-export5"
		uniquePostfix       = randString(7)

		queueResources = []QueueExport{
			{OriginalResourceLabel: "test-queue-prod-1", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod-" + uniquePostfix, Description: "This is a test prod queue 1", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-prod-2", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod-" + uniquePostfix, Description: "This is a test prod queue 2", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-prod-3", ExportedLabel: "test-queue-" + uuid.NewString() + "-prod-" + uniquePostfix, Description: "This is a test prod queue 3", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-test-4", ExportedLabel: "test-queue-" + uuid.NewString() + "-test-" + uniquePostfix, Description: "This is a test queue 4", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-dev-1", ExportedLabel: "test-queue-" + uuid.NewString() + "-dev-" + uniquePostfix, Description: "This is a dev queue 5", AcwTimeoutMs: 200000},
		}
	)
	defer os.RemoveAll(exportTestDir)

	queueResourceDef := buildQueueResources(queueResources)
	baseConfig := queueResourceDef
	configWithExporter := baseConfig + generateTfExportByExcludeFilterResources(
		exportResourceLabel,
		exportTestDir,
		util.TrueValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue::.*-(dev|test)"),
			strconv.Quote("genesyscloud_outbound_ruleset"),
			strconv.Quote("genesyscloud_user"),
			strconv.Quote("genesyscloud_user_roles"),
			strconv.Quote("genesyscloud_flow"),
		},
		strconv.Quote("json"),
		util.FalseValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue." + queueResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[2].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[3].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[4].OriginalResourceLabel),
		},
	)

	sanitizer := resourceExporter.NewSanitizerProvider()

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				// Generate a queue
				Config: baseConfig,
			},
			{
				// Now export them all
				Config: configWithExporter,
				Check: resource.ComposeTestCheckFunc(
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[0].ExportedLabel), queueResources[0]), //Want to make sure the prod queues are queue is there
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[1].ExportedLabel), queueResources[1]), //Want to make sure the prod queues are queue is there
					testQueueExportEqual(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", sanitizer.S.SanitizeResourceBlockLabel(queueResources[2].ExportedLabel), queueResources[2]), //Want to make sure the prod queues are queue is there
					testQueueExportExcludesRegEx(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_routing_queue", ".*-(dev|test)"),                                                                    //We should not find any "dev or test" queues here because we only wanted to include queues that ended with a -prod
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

func TestAccResourceTfExportFormAsHCL(t *testing.T) {
	var (
		exportTestDir     = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		exportedContents  string
		pathToHclFile     = filepath.Join(exportTestDir, defaultTfHCLFile)
		formName          = "terraform_form_evaluations_" + uuid.NewString()
		formResourceLabel = formName

		// Complete evaluation form
		evaluationForm1 = gcloud.EvaluationFormStruct{
			Name:      formName,
			Published: false,

			QuestionGroups: []gcloud.EvaluationFormQuestionGroupStruct{
				{
					Name:                    "Test Question Group 1",
					DefaultAnswersToHighest: true,
					DefaultAnswersToNA:      true,
					NaEnabled:               true,
					Weight:                  1,
					ManualWeight:            true,
					Questions: []gcloud.EvaluationFormQuestionStruct{
						{
							Text: "Did the agent perform the opening spiel?",
							AnswerOptions: []gcloud.AnswerOptionStruct{
								{
									Text:  "Yes",
									Value: 1,
								},
								{
									Text:  "No",
									Value: 0,
								},
							},
						},
						{
							Text:             "Did the agent greet the customer?",
							HelpText:         "Help text here",
							NaEnabled:        true,
							CommentsRequired: true,
							IsKill:           true,
							IsCritical:       true,
							VisibilityCondition: gcloud.VisibilityConditionStruct{
								CombiningOperation: "AND",
								Predicates:         []string{"/form/questionGroup/0/question/0/answer/0", "/form/questionGroup/0/question/0/answer/1"},
							},
							AnswerOptions: []gcloud.AnswerOptionStruct{
								{
									Text:  "Yes",
									Value: 1,
								},
								{
									Text:  "No",
									Value: 0,
								},
							},
						},
					},
				},
				{
					Name:   "Test Question Group 2",
					Weight: 2,
					Questions: []gcloud.EvaluationFormQuestionStruct{
						{
							Text: "Did the agent offer to sell product?",
							AnswerOptions: []gcloud.AnswerOptionStruct{
								{
									Text:  "Yes",
									Value: 1,
								},
								{
									Text:  "No",
									Value: 0,
								},
							},
						},
					},
					VisibilityCondition: gcloud.VisibilityConditionStruct{
						CombiningOperation: "AND",
						Predicates:         []string{"/form/questionGroup/0/question/0/answer/1"},
					},
				},
			},
		}
	)

	defer os.RemoveAll(exportTestDir)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: gcloud.GenerateEvaluationFormResource(formResourceLabel, &evaluationForm1),
				Check: resource.ComposeTestCheckFunc(
					validateEvaluationFormAttributes(formResourceLabel, evaluationForm1),
				),
			},
			{
				Config: gcloud.GenerateEvaluationFormResource(formResourceLabel, &evaluationForm1) + generateTfExportByFilter(
					formResourceLabel,
					exportTestDir,
					util.TrueValue,
					[]string{strconv.Quote("genesyscloud_quality_forms_evaluation::" + formName)},
					"",
					strconv.Quote("hcl"),
					util.FalseValue,
					[]string{
						strconv.Quote("genesyscloud_quality_forms_evaluation." + formResourceLabel),
					},
				),
				Check: resource.ComposeTestCheckFunc(
					getExportedFileContents(pathToHclFile, &exportedContents),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})

	exportedContents = removeTerraformProviderBlock(exportedContents)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: exportedContents,
				Check: resource.ComposeTestCheckFunc(
					validateEvaluationFormAttributes(formResourceLabel, evaluationForm1),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

func TestAccResourceTfExportQueueAsHCL(t *testing.T) {

	//t.Parallel()

	var (
		exportTestDir  = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		exportContents string
		pathToHclFile  = filepath.Join(exportTestDir, defaultTfHCLFile)
	)

	defer os.RemoveAll(exportTestDir)

	// routing queue attributes
	var (
		queueName      = fmt.Sprintf("queue_%v", uuid.NewString())
		queueLabel     = queueName
		description    = "This is a test queue"
		autoAnswerOnly = "true"

		alertTimeoutSec = "30"
		slPercentage    = "0.7"
		slDurationMs    = "10000"

		rrOperator    = "MEETS_THRESHOLD"
		rrThreshold   = "9"
		rrWaitSeconds = "300"

		chatScriptID  = uuid.NewString()
		emailScriptID = uuid.NewString()
	)

	routingQueue := routingQueue.GenerateRoutingQueueResource(
		queueLabel,
		queueName,
		description,
		strconv.Quote("MANDATORY_TIMEOUT"),
		"300000",
		strconv.Quote("BEST"),
		autoAnswerOnly,
		strconv.Quote("Example Inc."),
		util.NullValue,
		"true",
		"true",
		util.TrueValue,
		util.FalseValue,
		strconv.Quote("TimestampAndPriority"),
		util.NullValue,
		util.NullValue,
		routingQueue.GenerateMediaSettings("media_settings_call", alertTimeoutSec, util.FalseValue, slPercentage, slDurationMs),
		routingQueue.GenerateRoutingRules(rrOperator, rrThreshold, rrWaitSeconds),
		routingQueue.GenerateDefaultScriptIDs(chatScriptID, emailScriptID),
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: routingQueue,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("genesyscloud_routing_queue."+queueLabel, "name", queueName),
					resource.TestCheckResourceAttr("genesyscloud_routing_queue."+queueLabel, "description", description),
					resource.TestCheckResourceAttr("genesyscloud_routing_queue."+queueLabel, "auto_answer_only", "true"),
					resource.TestCheckResourceAttr("genesyscloud_routing_queue."+queueLabel, "default_script_ids.CHAT", chatScriptID),
					resource.TestCheckResourceAttr("genesyscloud_routing_queue."+queueLabel, "default_script_ids.EMAIL", emailScriptID),
					validateMediaSettings(queueLabel, "media_settings_call", alertTimeoutSec, slPercentage, slDurationMs),
					validateRoutingRules(queueLabel, 0, rrOperator, rrThreshold, rrWaitSeconds),
				),
			},
			{
				Config: routingQueue + generateTfExportByFilter("export",
					exportTestDir,
					util.TrueValue,
					[]string{strconv.Quote("genesyscloud_routing_queue::" + queueName)},
					"",
					strconv.Quote("hcl"),
					util.FalseValue,
					[]string{
						strconv.Quote("genesyscloud_routing_queue." + queueLabel),
					},
				),
				Check: resource.ComposeTestCheckFunc(
					getExportedFileContents(pathToHclFile, &exportContents),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})

	exportContents = removeTerraformProviderBlock(exportContents)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: exportContents,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("genesyscloud_routing_queue."+queueLabel, "name", queueName),
					resource.TestCheckResourceAttr("genesyscloud_routing_queue."+queueLabel, "description", description),
					resource.TestCheckResourceAttr("genesyscloud_routing_queue."+queueLabel, "auto_answer_only", "true"),
					resource.TestCheckResourceAttr("genesyscloud_routing_queue."+queueLabel, "default_script_ids.CHAT", chatScriptID),
					resource.TestCheckResourceAttr("genesyscloud_routing_queue."+queueLabel, "default_script_ids.EMAIL", emailScriptID),
					validateMediaSettings(queueLabel, "media_settings_call", alertTimeoutSec, slPercentage, slDurationMs),
					validateRoutingRules(queueLabel, 0, rrOperator, rrThreshold, rrWaitSeconds),
				),
			},
		},
	})
}

func TestAccResourceTfExportLogMissingPermissions(t *testing.T) {
	var (
		exportTestDir           = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		configPath              = filepath.Join(exportTestDir, defaultTfJSONFile)
		permissionsErrorMessage = "API Error 403 - Missing view permissions."
		otherErrorMessage       = "API Error 411 - Another type of error."

		err1    = diag.Diagnostic{Summary: permissionsErrorMessage}
		err2    = diag.Diagnostic{Summary: otherErrorMessage}
		errors1 = diag.Diagnostics{err1}
		errors2 = diag.Diagnostics{err1, err2}
	)

	defer os.RemoveAll(exportTestDir)

	mockError = errors1

	// Checking that the config file is created when the error is 403 & log_permission_errors = true
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: generateTfExportByFilter("test-export",
					exportTestDir,
					util.FalseValue,
					[]string{strconv.Quote("genesyscloud_quality_forms_evaluation")},
					"",
					strconv.Quote("json"),
					util.TrueValue,
					[]string{}),
				Check: resource.ComposeTestCheckFunc(
					validateFileCreated(configPath),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})

	// Check that the export fails when a non-403 error exists
	mockError = errors2

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: generateTfExportByFilter("test-export",
					exportTestDir,
					util.FalseValue,
					[]string{strconv.Quote("genesyscloud_quality_forms_evaluation")},
					"",
					strconv.Quote("json"),
					util.TrueValue,
					[]string{}),
				ExpectError: regexp.MustCompile(otherErrorMessage),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})

	// Check that info about attr exists in error summary when 403 is found & log_permission_errors = false
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: generateTfExportByFilter("test-export",
					exportTestDir,
					util.FalseValue,
					[]string{strconv.Quote("genesyscloud_quality_forms_evaluation")},
					"",
					strconv.Quote("json"),
					util.FalseValue,
					[]string{}),
				ExpectError: regexp.MustCompile(logAttrInfo),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})

	// Clean up
	mockError = nil
}

func TestAccResourceTfExportUserPromptExportAudioFile(t *testing.T) {
	var (
		userPromptResourceLabel     = "test_prompt"
		userPromptName              = "TestPrompt" + strings.Replace(uuid.NewString(), "-", "", -1)
		userPromptDescription       = "Test description"
		userPromptResourceLanguage  = "en-us"
		userPromptResourceText      = "This is a test greeting!"
		userResourcePromptFilename1 = testrunner.GetTestDataPath("resource", userPromptResource.ResourceType, "test-prompt-01.wav")
		userResourcePromptFilename2 = testrunner.GetTestDataPath("resource", userPromptResource.ResourceType, "test-prompt-02.wav")

		userPromptResourceLanguage2 = "pt-br"
		userPromptResourceText2     = "This is a test greeting!!!"

		exportResourceLabel = "export"
		exportTestDir       = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
	)

	userPromptAsset := userPrompt.UserPromptResourceStruct{
		Language:        userPromptResourceLanguage,
		Tts_string:      util.NullValue,
		Text:            strconv.Quote(userPromptResourceText),
		Filename:        strconv.Quote(userResourcePromptFilename1),
		FileContentHash: userResourcePromptFilename1,
	}

	userPromptAsset2 := userPrompt.UserPromptResourceStruct{
		Language:        userPromptResourceLanguage2,
		Tts_string:      util.NullValue,
		Text:            strconv.Quote(userPromptResourceText2),
		Filename:        strconv.Quote(userResourcePromptFilename2),
		FileContentHash: userResourcePromptFilename2,
	}

	userPromptResources := []*userPrompt.UserPromptResourceStruct{&userPromptAsset}
	userPromptResources2 := []*userPrompt.UserPromptResourceStruct{&userPromptAsset, &userPromptAsset2}

	defer os.RemoveAll(exportTestDir)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: userPrompt.GenerateUserPromptResource(&userPrompt.UserPromptStruct{
					ResourceLabel: userPromptResourceLabel,
					Name:          userPromptName,
					Description:   strconv.Quote(userPromptDescription),
					Resources:     userPromptResources,
				}),
			},
			{
				Config: generateTfExportByFilter(
					exportResourceLabel,
					exportTestDir,
					util.FalseValue,
					[]string{strconv.Quote("genesyscloud_architect_user_prompt::" + userPromptName)},
					"",
					strconv.Quote("json"),
					util.FalseValue,
					[]string{
						strconv.Quote("genesyscloud_architect_user_prompt." + userPromptResourceLabel),
					},
				) + userPrompt.GenerateUserPromptResource(&userPrompt.UserPromptStruct{
					ResourceLabel: userPromptResourceLabel,
					Name:          userPromptName,
					Description:   strconv.Quote(userPromptDescription),
					Resources:     userPromptResources,
				}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("genesyscloud_architect_user_prompt."+userPromptResourceLabel, "name", userPromptName),
					resource.TestCheckResourceAttr("genesyscloud_architect_user_prompt."+userPromptResourceLabel, "description", userPromptDescription),
					resource.TestCheckResourceAttr("genesyscloud_architect_user_prompt."+userPromptResourceLabel, "resources.0.language", userPromptResourceLanguage),
					resource.TestCheckResourceAttr("genesyscloud_architect_user_prompt."+userPromptResourceLabel, "resources.0.text", userPromptResourceText),
					resource.TestCheckResourceAttr("genesyscloud_architect_user_prompt."+userPromptResourceLabel, "resources.0.filename", userResourcePromptFilename1),
					testUserPromptAudioFileExport(filepath.Join(exportTestDir, defaultTfJSONFile), "genesyscloud_architect_user_prompt", userPromptResourceLabel, exportTestDir, userPromptName),
				),
			},
			// Update to two resources with separate audio files
			{
				Config: userPrompt.GenerateUserPromptResource(&userPrompt.UserPromptStruct{
					ResourceLabel: userPromptResourceLabel,
					Name:          userPromptName,
					Description:   strconv.Quote(userPromptDescription),
					Resources:     userPromptResources2,
				}),
				ExpectNonEmptyPlan: true,
			},
			{
				Config: generateTfExportByFilter(
					exportResourceLabel,
					exportTestDir,
					util.FalseValue,
					[]string{strconv.Quote("genesyscloud_architect_user_prompt::" + userPromptName)},
					"",
					strconv.Quote("json"),
					util.FalseValue,
					[]string{
						strconv.Quote("genesyscloud_architect_user_prompt." + userPromptResourceLabel),
					},
				) + userPrompt.GenerateUserPromptResource(&userPrompt.UserPromptStruct{
					ResourceLabel: userPromptResourceLabel,
					Name:          userPromptName,
					Description:   strconv.Quote(userPromptDescription),
					Resources:     userPromptResources2,
				}),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("genesyscloud_architect_user_prompt."+userPromptResourceLabel, "name", userPromptName),
					resource.TestCheckResourceAttr("genesyscloud_architect_user_prompt."+userPromptResourceLabel, "description", userPromptDescription),
					resource.TestCheckResourceAttr("genesyscloud_architect_user_prompt."+userPromptResourceLabel, "resources.0.language", userPromptResourceLanguage),
					resource.TestCheckResourceAttr("genesyscloud_architect_user_prompt."+userPromptResourceLabel, "resources.0.text", userPromptResourceText),
					resource.TestCheckResourceAttr("genesyscloud_architect_user_prompt."+userPromptResourceLabel, "resources.0.filename", userResourcePromptFilename1),
					testUserPromptAudioFileExport(filepath.Join(exportTestDir, defaultTfJSONFile), "genesyscloud_architect_user_prompt", userPromptResourceLabel, exportTestDir, userPromptName),
				),
			},
			{
				ResourceName:            "genesyscloud_architect_user_prompt." + userPromptResourceLabel,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"resources"},
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

func TestAccResourceSurveyFormsPublishedAndUnpublished(t *testing.T) {
	var (
		exportTestDir = testrunner.GetTestTempPath(".terraformregex" + uuid.NewString())
		resourceLabel = "export"
		configPath    = filepath.Join(exportTestDir, defaultTfJSONFile)
		statePath     = filepath.Join(exportTestDir, defaultTfStateFile)
	)

	// Clean up
	defer func(path string) {
		if err := os.RemoveAll(path); err != nil {
			t.Logf("failed to remove dir %s: %s", path, err)
		}
	}(exportTestDir)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: generateTfExportByIncludeFilterResources(
					resourceLabel,
					exportTestDir,
					util.TrueValue, // include_state_file
					[]string{ // include_filter_resources
						strconv.Quote("genesyscloud_quality_forms_survey"),
					},
					strconv.Quote("json"), // export_as_hcl
					util.FalseValue,
					[]string{},
				),
				Check: resource.ComposeTestCheckFunc(
					validatePublishedAndUnpublishedExported(configPath),
					validateStateFileHasPublishedAndUnpublished(statePath),
				),
			},
		},
	})
}

// TestAccResourceExportManagedSitesAsData checks that during an export, managed sites are exported as data source
// Managed can't be set on sites, therefore the default managed site is checked during the test that it is exported as data
func TestAccResourceExportManagedSitesAsData(t *testing.T) {
	var (
		exportTestDir = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		resourceLabel = "export"
		configPath    = filepath.Join(exportTestDir, defaultTfJSONFile)
		statePath     = filepath.Join(exportTestDir, defaultTfStateFile)
		siteName      = "PureCloud Voice - AWS"
	)

	if err := telephonyProvidersEdgesSite.CheckForDefaultSite(siteName); err != nil {
		t.Skipf("failed to get default site %v", err)
	}

	platform := platform.GetPlatform()
	if platform.IsDevelopmentPlatform() {
		t.Skip("Skipping test for development platform due to inability to properly convert statefile to v4 within development platform")
	}

	defer func(path string) {
		if err := os.RemoveAll(path); err != nil {
			t.Logf("failed to remove dir %s: %s", path, err)
		}
	}(exportTestDir)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: generateTfExportByIncludeFilterResources(
					resourceLabel,
					exportTestDir,
					util.TrueValue, // include_state_file
					[]string{ // include_filter_resources
						strconv.Quote("genesyscloud_telephony_providers_edges_site"),
					},
					strconv.Quote("json"), // export_format attribute
					util.FalseValue,
					[]string{},
				),
				Check: resource.ComposeTestCheckFunc(
					validateStateFileAsData(statePath, siteName),
					validateExportManagedSitesAsData(configPath, siteName),
				),
			},
		},
	})
}

// TestAccResourceTfExportSplitFilesAsHCL will create 2 queues, 2 wrap up codes, and 2 users.
// The exporter will be run in split mode so 3 resource tfs should be created as well as a provider.tf
func TestAccResourceTfExportSplitFilesAsHCL(t *testing.T) {
	var (
		exportTestDir       = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		exportResourceLabel = "test-export-split"
		uniquePostfix       = randString(7)
		expectedFilesPath   = []string{
			filepath.Join(exportTestDir, "genesyscloud_routing_queue.tf"),
			filepath.Join(exportTestDir, "genesyscloud_user.tf"),
			filepath.Join(exportTestDir, "genesyscloud_routing_wrapupcode.tf"),
			filepath.Join(exportTestDir, "provider.tf"),
		}

		queueResources = []QueueExport{
			{OriginalResourceLabel: "test-queue-1", ExportedLabel: "test-queue-1-" + uuid.NewString() + uniquePostfix, Description: "This is a test queue", AcwTimeoutMs: 200000},
			{OriginalResourceLabel: "test-queue-2", ExportedLabel: "test-queue-1-" + uuid.NewString() + uniquePostfix, Description: "This is a test queue too", AcwTimeoutMs: 200000},
		}

		userResources = []UserExport{
			{OriginalResourceLabel: "test-user-1", ExportedLabel: "test-user-1", Email: "test-user-1" + uuid.NewString() + "@test.com" + uniquePostfix, State: "active"},
			{OriginalResourceLabel: "test-user-2", ExportedLabel: "test-user-2", Email: "test-user-2" + uuid.NewString() + "@test.com" + uniquePostfix, State: "active"},
		}

		wrapupCodeResources = []WrapupcodeExport{
			{OriginalResourceLabel: "test-wrapupcode-1", Name: "test-wrapupcode-1-" + uuid.NewString() + uniquePostfix},
			{OriginalResourceLabel: "test-wrapupcode-2", Name: "test-wrapupcode-2-" + uuid.NewString() + uniquePostfix},
		}

		divResourceLabel = "test-division"
		divName          = "terraform-" + uuid.NewString()
		description      = "Terraform wrapup code description"
	)
	defer os.RemoveAll(exportTestDir)

	queueResourceDef := buildQueueResources(queueResources)
	userResourcesDef := buildUserResources(userResources)

	wrapupcodeResourceDef := buildWrapupcodeResources(wrapupCodeResources, "genesyscloud_auth_division."+divResourceLabel+".id", description)
	baseConfig := queueResourceDef + authDivision.GenerateAuthDivisionBasic(divResourceLabel, divName) + wrapupcodeResourceDef + userResourcesDef
	configWithExporter := baseConfig + generateTfExportByIncludeFilterResources(
		exportResourceLabel,
		exportTestDir,
		util.TrueValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue::" + uniquePostfix + "$"),
			strconv.Quote("genesyscloud_user::" + uniquePostfix + "$"),
			strconv.Quote("genesyscloud_routing_wrapupcode::" + uniquePostfix + "$"),
		},
		strconv.Quote("hcl"),
		util.TrueValue,
		[]string{
			strconv.Quote("genesyscloud_routing_queue." + queueResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_queue." + queueResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_user." + userResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_user." + userResources[1].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[0].OriginalResourceLabel),
			strconv.Quote("genesyscloud_routing_wrapupcode." + wrapupCodeResources[1].OriginalResourceLabel),
		},
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: baseConfig,
			},
			{
				Config: configWithExporter,
				Check: resource.ComposeTestCheckFunc(
					validateFileCreated(expectedFilesPath[0]),
					validateFileCreated(expectedFilesPath[1]),
					validateFileCreated(expectedFilesPath[2]),
					validateFileCreated(expectedFilesPath[3]),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

func validateStateFileHasPublishedAndUnpublished(filename string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		if _, err := os.Stat(filename); err != nil {
			return fmt.Errorf("failed to find file %s", filename)
		}

		stateData, err := loadJsonFileToMap(filename)
		if err != nil {
			return err
		}

		modules, ok := stateData["modules"].([]interface{})
		if !ok {
			return fmt.Errorf("unexpected structure for modules")
		}

		log.Println("Successfully loaded export config into map variable ")

		resources := make([]interface{}, 0, len(modules))
		for _, module := range modules {
			m, ok := module.(map[string]interface{})
			if !ok {
				continue
			}
			if r, ok := m["resources"].([]interface{}); ok {
				resources = append(resources, r...)
			}
		}

		log.Printf("checking that quality forms surveys exports published and unpublished in tf state")

		for _, r := range resources {
			out, ok := r.(map[string]interface{})
			if !ok {
				continue
			}

			instances, ok := out["instances"].([]interface{})
			if !ok {
				return fmt.Errorf("unexpected structure for form %s", filename)
			}

			res, ok := instances[0].(map[string]interface{})["attributes_flat"].(map[string]interface{})
			if !ok {
				return fmt.Errorf("unexpected structure attributes %s", filename)
			}

			name, ok := res["name"].(string)
			if !ok {
				return fmt.Errorf("unexpected name structure for form %s", filename)
			}

			published, ok := res["published"].(string)
			if !ok {
				return fmt.Errorf("unexpected published structure for form %s", filename)
			}

			if name == "test-published-form" {
				if published == "true" {
					log.Printf("Form with name '%s' is correctly exported as published\n", name)
				} else {
					return fmt.Errorf("Form with name '%s' is not correctly exported as published\n", name)
				}
			}

			if name == "test-unpublished-form" {
				if published == "false" {
					log.Printf("Form with name '%s' is correctly exported as unpublished\n", name)
				} else {
					return fmt.Errorf("Form with name '%s' is not correctly exported as unpublished\n", name)
				}
			}
		}

		return nil
	}
}

// validateStateFileAsData verifies that the default managed site 'PureCloud Voice - AWS' is exported as a data source
func validateStateFileAsData(filename, siteName string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		_, err := os.Stat(filename)
		if err != nil {
			return fmt.Errorf("failed to find file %s", filename)
		}

		stateData, err := loadJsonFileToMap(filename)
		if err != nil {
			return err
		}
		log.Println("Successfully loaded export config into map variable ")

		// Check if data sources exist in the exported data
		if resources, ok := stateData["resources"].([]interface{}); ok {
			fmt.Printf("checking that managed site with name %s is exported as data source in tf state\n", siteName)

			// Validate each site's name
			for _, r := range resources {
				fmt.Printf("resource that managed site with name %v is exported as data source\n", r)

				res, ok := r.(map[string]interface{})
				if !ok {
					return fmt.Errorf("unexpected structure for site %s", siteName)
				}

				name, ok := res["name"].(string)
				if !ok {
					return fmt.Errorf("unexpected structure for site %s", siteName)
				}

				mode, ok := res["mode"].(string)
				if !ok {
					return fmt.Errorf("unexpected structure for site %s", siteName)
				}

				if name == strings.ReplaceAll(siteName, " ", "_") {
					if mode == "data" {
						log.Printf("Site with name '%s' is correctly exported as data source\n", siteName)
						return nil
					} else {
						log.Printf("Site with name '%s' is not correctly exported as data\n", siteName)
						return nil
					}
				}
			}
			return fmt.Errorf("No Resources '%s' was not exported as data source", siteName)
		} else {
			return fmt.Errorf("No data sources found in exported data")
		}
	}
}

func validatePublishedAndUnpublishedExported(configFile string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		_, err := os.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("failed to read state file %s: %v", configFile, err)
		}

		// Load the JSON content of the export file
		log.Println("Loading export config into map variable")
		exportData, err := loadJsonFileToMap(configFile)
		if err != nil {
			return err
		}

		if data, ok := exportData["resource"].(map[string]interface{}); ok {
			forms, ok := data["genesyscloud_quality_forms_survey"].(map[string]interface{})
			if !ok {
				return fmt.Errorf("no resources exported for genesyscloud_quality_forms_survey")
			}

			if publishedForm, ok := forms["test-published-form"].(map[string]interface{}); ok {
				if publishedForm["published"].(bool) != true {
					return fmt.Errorf("test-published-form is not published")
				}
			} else {
				return fmt.Errorf("test-published-form is not exported")
			}
			if unpublishedForm, ok := forms["test-unpublished-form"].(map[string]interface{}); ok {
				if unpublishedForm["published"].(bool) != false {
					return fmt.Errorf("test-unpublished-form is published")
				}
			} else {
				return fmt.Errorf("test-unpublished-form is not exported")
			}
		}
		return nil
	}
}

// validateExportManagedSitesAsData verifies that the default managed site 'PureCloud Voice - AWS' is exported as a data source
func validateExportManagedSitesAsData(filename, siteName string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		// Check if the file exists
		_, err := os.Stat(filename)
		if err != nil {
			return fmt.Errorf("failed to find file %s", filename)
		}

		// Load the JSON content of the export file
		log.Println("Loading export config into map variable")
		exportData, err := loadJsonFileToMap(filename)
		if err != nil {
			return err
		}
		log.Println("Successfully loaded export config into map variable ")

		// Check if data sources exist in the exported data
		if data, ok := exportData["data"].(map[string]interface{}); ok {
			sites, ok := data["genesyscloud_telephony_providers_edges_site"].(map[string]interface{})
			if !ok {
				return fmt.Errorf("no data sources exported for genesyscloud_telephony_providers_edges_site")
			}

			log.Printf("checking that managed site with name %s is exported as data source\n", siteName)

			// Validate each site's name
			for siteID, site := range sites {
				siteAttributes, ok := site.(map[string]interface{})
				if !ok {
					return fmt.Errorf("unexpected structure for site %s", siteID)
				}

				name, _ := siteAttributes["name"].(string)
				if name == siteName {
					log.Printf("Site %s with name '%s' is correctly exported as data source", siteID, siteName)
					return nil
				}
			}
			return fmt.Errorf("site with name '%s' was not exported as data source", siteName)
		} else {
			return fmt.Errorf("no data sources found in exported data")
		}
	}
}

func validatePromptsExported(filename string, expectedPrompts []string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		_, err := os.Stat(filename + "/genesyscloud.tf.json")
		if err != nil {
			return fmt.Errorf("failed to find file %s", filename)
		}

		log.Println("Loading export config into map variable")
		exportData, err := loadJsonFileToMap(filename + "/genesyscloud.tf.json")
		if err != nil {
			return fmt.Errorf("failed to unmarshal JSON from file %s: %s", filename, err)
		}
		log.Println("Successfully loaded export config into map variable")

		resources, ok := exportData["resource"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("no 'resource' section found in exported JSON")
		}
		prompts, ok := resources["genesyscloud_architect_user_prompt"].(map[string]interface{})
		if !ok {
			return fmt.Errorf("no resources exported for genesyscloud_architect_user_prompt")
		}

		for _, promptID := range expectedPrompts {
			if promptData, found := prompts[promptID]; found {
				promptAttributes, ok := promptData.(map[string]interface{})
				if !ok {
					return fmt.Errorf("unexpected structure for prompt %s", promptID)
				}

				// Check that the "name" attribute matches the expected prompt name
				name, _ := promptAttributes["name"].(string)
				log.Printf("Verifying prompt '%s' with name '%s'", promptID, name)
				if name != promptID {
					return fmt.Errorf("expected prompt name '%s' but found '%s'", promptID, name)
				}
			} else {
				return fmt.Errorf("expected prompt with ID '%s' not found in exported data", promptID)
			}
		}

		log.Println("All expected prompts are correctly exported")
		return nil
	}
}

// TestAccResourceUserPromptsExported tests the new getAll functionality where it adds the name filter and makes a call per letter
// This will prevent the 10,000 return limit being hit on export and not returning everything
func TestAccResourceTfExportUserPromptsExported(t *testing.T) {
	var (
		uniqueStr            = strings.Replace(uuid.NewString(), "-", "_", -1)
		exportTestDir        = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		resourceID           = "export"
		dPromptResourceLabel = "d_user_prompt"
		dPromptNameAttr      = "d_user_prompt_test" + uniqueStr
		hPromptResourceLabel = "h_user_prompt"
		hPromptNameAttr      = "h_user_prompt_test" + uniqueStr
		zPromptResourceLabel = "Z_user_prompt"
		zPromptNameAttr      = "Z_user_prompt_test" + uniqueStr

		allNames = []string{dPromptNameAttr, hPromptNameAttr, zPromptNameAttr}
	)

	promptConfig := fmt.Sprintf(`
resource "genesyscloud_architect_user_prompt" "%s" {
	name        = "%s"
	description = "Welcome greeting for all callers"
	resources {
		language   = "en-us"
		text       = "Good day. Thank you for calling."
		tts_string = "Good day. Thank you for calling."
	}
}

resource "genesyscloud_architect_user_prompt" "%s" {
	name        = "%s"
	description = "Welcome greeting for all callers"
	resources {
		language   = "en-us"
		text       = "Good day. Thank you for calling."
		tts_string = "Good day. Thank you for calling."
	}
}

resource "genesyscloud_architect_user_prompt" "%s" {
	name        = "%s"
	description = "Welcome greeting for all callers"
	resources {
		language   = "en-us"
		text       = "Good day. Thank you for calling."
		tts_string = "Good day. Thank you for calling."
	}
}
`, dPromptResourceLabel, dPromptNameAttr, hPromptResourceLabel, hPromptNameAttr, zPromptResourceLabel, zPromptNameAttr)

	defer func(path string) {
		if err := os.RemoveAll(path); err != nil {
			t.Logf("failed to remove dir %s: %s", path, err)
		}
	}(exportTestDir)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: promptConfig,
			},
			{
				Config: promptConfig + generateTfExportByIncludeFilterResources(
					resourceID,
					exportTestDir,
					util.TrueValue, // include_state_file
					[]string{ // include_filter_resources
						strconv.Quote("genesyscloud_architect_user_prompt::" + dPromptNameAttr),
						strconv.Quote("genesyscloud_architect_user_prompt::" + hPromptNameAttr),
						strconv.Quote("genesyscloud_architect_user_prompt::" + zPromptNameAttr),
					},
					strconv.Quote("json"), // export_format attribute
					util.FalseValue,
					[]string{
						strconv.Quote("genesyscloud_architect_user_prompt." + dPromptResourceLabel),
						strconv.Quote("genesyscloud_architect_user_prompt." + hPromptResourceLabel),
						strconv.Quote("genesyscloud_architect_user_prompt." + zPromptResourceLabel),
					},
				),
				Check: resource.ComposeTestCheckFunc(
					validatePromptsExported(exportTestDir, allNames),
				),
			},
		},
	})
}

// TestAccResourceTfExportCampaignScriptIdReferences exports two campaigns and ensures that the custom revolver OutboundCampaignAgentScriptResolver
// is working properly i.e. script_id should reference a data source pointing to the Default Outbound Script under particular circumstances
func TestAccResourceTfExportCampaignScriptIdReferences(t *testing.T) {
	var (
		exportTestDir = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		resourceLabel = "export"

		campaignNameDefaultScript          = "tf test df campaign " + uuid.NewString()
		campaignResourceLabelDefaultScript = strings.Replace(campaignNameDefaultScript, " ", "_", -1)

		campaignNameCustomScript          = "tf test ct campaign " + uuid.NewString()
		campaignResourceLabelCustomScript = strings.Replace(campaignNameCustomScript, " ", "_", -1)

		contactListResourceLabel = "contact_list"
		contactListName          = "tf test contact list " + uuid.NewString()
		queueName                = "tf test queue " + uuid.NewString()

		scriptName            = "tf_test_script_" + uuid.NewString()
		pathToScriptFile      = testrunner.GetTestDataPath("resource", "genesyscloud_script", "test_script.json")
		fullyQualifiedPath, _ = filepath.Abs(pathToScriptFile)

		configPath = filepath.Join(exportTestDir, defaultTfJSONFile)
	)

	contactListConfig := fmt.Sprintf(`
resource "genesyscloud_outbound_contact_list" "%s" {
  name         = "%s"
  column_names = ["First Name", "Last Name", "Cell", "Home"]
  phone_columns {
    column_name = "Cell"
    type        = "cell"
  }
  phone_columns {
    column_name = "Home"
    type        = "home"
  }
}
`, contactListResourceLabel, contactListName)

	remainingConfig := fmt.Sprintf(`
data "genesyscloud_script" "default" {
	name = "Default Outbound Script"
}

resource "genesyscloud_outbound_campaign" "%s" {
  name            = "%s"
  queue_id        = "${genesyscloud_routing_queue.queue.id}"
  caller_address  = "+13335551234"
  contact_list_id = "${genesyscloud_outbound_contact_list.%s.id}"
  dialing_mode    = "preview"
  script_id       = "${data.genesyscloud_script.default.id}"
  caller_name     = "Callbacks Test Queue 1"
  division_id     = "${data.genesyscloud_auth_division_home.home.id}"
  campaign_status = "off"
  dynamic_contact_queueing_settings {
    sort = false
  }
  phone_columns {
    column_name = "Cell"
  }
}

resource "genesyscloud_outbound_campaign" "%s" {
  name            = "%s"
  queue_id        = "${genesyscloud_routing_queue.queue.id}"
  caller_address  = "+13335551234"
  contact_list_id = "${genesyscloud_outbound_contact_list.%s.id}"
  dialing_mode    = "preview"
  script_id       = "${genesyscloud_script.script.id}"
  caller_name     = "Callbacks Test Queue 1"
  division_id     = "${data.genesyscloud_auth_division_home.home.id}"
  campaign_status = "off"
  dynamic_contact_queueing_settings {
    sort = false
  }
  phone_columns {
    column_name = "Cell"
  }
}

resource "genesyscloud_script" "script" {
	script_name       = "%s"
	filepath          = "%s"
	file_content_hash = filesha256("%s")
}

data "genesyscloud_auth_division_home" "home" {}

resource "genesyscloud_routing_queue" "queue" {
  name = "%s"
}
`, campaignResourceLabelDefaultScript,
		campaignNameDefaultScript,
		contactListResourceLabel,
		campaignResourceLabelCustomScript,
		campaignNameCustomScript,
		contactListResourceLabel,
		scriptName,
		pathToScriptFile,
		fullyQualifiedPath,
		queueName)

	defer func(path string) {
		if err := os.RemoveAll(path); err != nil {
			t.Logf("failed to remove dir %s: %s", path, err)
		}
	}(exportTestDir)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: contactListConfig, // need to add contact list first to seed it with contacts before creating the campaigns
				Check:  addContactsToContactList,
			},
			{
				Config: remainingConfig + contactListConfig,
			},
			// Verify script_id fields are resolved properly when include_state_file = false and enable_dependency_resolution = true
			{
				Config: remainingConfig + contactListConfig + generateExportResourceIncludeFilterWithEnableDepRes(
					resourceLabel,
					exportTestDir,
					util.FalseValue,       // include_state_file
					strconv.Quote("json"), // export_format attribute
					util.TrueValue,        // enable_dependency_resolution
					[]string{ // include_filter_resources
						strconv.Quote("genesyscloud_outbound_campaign::" + campaignNameDefaultScript),
						strconv.Quote("genesyscloud_outbound_campaign::" + campaignNameCustomScript),
					},
					[]string{ // depends_on
						"genesyscloud_outbound_campaign." + campaignResourceLabelDefaultScript,
						"genesyscloud_outbound_campaign." + campaignResourceLabelCustomScript,
					},
				),
				Check: resource.ComposeTestCheckFunc(
					validateExportedCampaignScriptIds(
						configPath,
						campaignResourceLabelCustomScript,
						campaignResourceLabelDefaultScript,
						"${data.genesyscloud_script.Default_Outbound_Script.id}",
						fmt.Sprintf("${genesyscloud_script.%s.id}", scriptName),
						true,
					),
					validateNumberOfExportedDataSources(configPath),
				),
			},
			// Verify script_id fields are resolved properly when include_state_file = true and enable_dependency_resolution = true
			{
				Config: remainingConfig + contactListConfig + generateExportResourceIncludeFilterWithEnableDepRes(
					resourceLabel,
					exportTestDir,
					util.TrueValue,        // include_state_file
					strconv.Quote("json"), // export_format attribute
					util.TrueValue,        // enable_dependency_resolution
					[]string{ // include_filter_resources
						strconv.Quote("genesyscloud_outbound_campaign::" + campaignNameDefaultScript),
						strconv.Quote("genesyscloud_outbound_campaign::" + campaignNameCustomScript),
					},
					[]string{ // depends_on
						"genesyscloud_outbound_campaign." + campaignResourceLabelDefaultScript,
						"genesyscloud_outbound_campaign." + campaignResourceLabelCustomScript,
					},
				),
				Check: resource.ComposeTestCheckFunc(
					validateExportedCampaignScriptIds(
						configPath,
						campaignResourceLabelCustomScript,
						campaignResourceLabelDefaultScript,
						"${data.genesyscloud_script.Default_Outbound_Script.id}",
						fmt.Sprintf("${genesyscloud_script.%s.id}", scriptName),
						true,
					),
					validateNumberOfExportedDataSources(configPath),
				),
			},
			// Verify script_id fields are resolved properly when include_state_file = true and enable_dependency_resolution = false
			{
				Config: remainingConfig + contactListConfig + generateExportResourceIncludeFilterWithEnableDepRes(
					resourceLabel,
					exportTestDir,
					util.TrueValue,        // include_state_file
					strconv.Quote("json"), // export_format attribute
					util.FalseValue,       // enable_dependency_resolution
					[]string{ // include_filter_resources
						strconv.Quote("genesyscloud_outbound_campaign::" + campaignNameDefaultScript),
						strconv.Quote("genesyscloud_outbound_campaign::" + campaignNameCustomScript),
					},
					[]string{ // depends_on
						"genesyscloud_outbound_campaign." + campaignResourceLabelDefaultScript,
						"genesyscloud_outbound_campaign." + campaignResourceLabelCustomScript,
					},
				),
				Check: resource.ComposeTestCheckFunc(
					validateExportedCampaignScriptIds(
						configPath,
						campaignResourceLabelCustomScript,
						campaignResourceLabelDefaultScript,
						"${data.genesyscloud_script.Default_Outbound_Script.id}",
						"",
						false,
					),
					validateNumberOfExportedDataSources(configPath),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

func removeTerraformProviderBlock(export string) string {
	return strings.Replace(export, terraformHCLBlock, "", -1)
}

// validateExportedCampaignScriptIds loads the exported content and validates that the custom resolver function
// resolved the script_id attr to the Default Outbound Script data source, and did not affect the campaign with a custom-made script
func validateExportedCampaignScriptIds(
	filename,
	customCampaignResourceLabel,
	defaultCampaignResourceLabel,
	expectedValueForCampaignWithDefaultScript,
	expectedValueForCampaignWithCustomScript string,
	verifyCustomScriptIdValue bool,
) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		_, err := os.Stat(filename)
		if err != nil {
			return fmt.Errorf("failed to find file %s", filename)
		}

		log.Println("Loading export config into map variable")
		exportData, err := loadJsonFileToMap(filename)
		if err != nil {
			return err
		}
		log.Println("Successfully loaded export config into map variable")

		if resources, ok := exportData["resource"].(map[string]interface{}); ok {
			campaigns, ok := resources["genesyscloud_outbound_campaign"].(map[string]interface{})
			if !ok {
				return fmt.Errorf("no campaign resources exported")
			}

			log.Println("Checking that campaign script_id values were resolved as expected")

			customCampaign, ok := campaigns[customCampaignResourceLabel].(map[string]interface{})
			if !ok {
				return fmt.Errorf("campaign with custom script was not exported")
			}

			if verifyCustomScriptIdValue {
				customCampaignScriptId, _ := customCampaign["script_id"].(string)
				if customCampaignScriptId != expectedValueForCampaignWithCustomScript {
					return fmt.Errorf("expected script ID to be '%s' for campaign with custom script, got '%s'", expectedValueForCampaignWithCustomScript, customCampaignScriptId)
				}
			}

			defaultCampaign, ok := campaigns[defaultCampaignResourceLabel].(map[string]interface{})
			if !ok {
				return fmt.Errorf("campaign with Default Outbound Script was not exported")
			}
			defaultCampaignScriptId, _ := defaultCampaign["script_id"].(string)
			if defaultCampaignScriptId != expectedValueForCampaignWithDefaultScript {
				return fmt.Errorf("expected script ID to be '%s' for campaign with default script, got '%s'", expectedValueForCampaignWithDefaultScript, defaultCampaignScriptId)
			}

			log.Println("Successfully verified that campaign script_ids were resolved correctly.")
		}

		return nil
	}
}

// validateNumberOfExportedDataSources validates that exactly one script data source is exported
func validateNumberOfExportedDataSources(filename string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		jsonFile, err := os.Open(filename)
		if err != nil {
			return fmt.Errorf("failed to open export file at path %s: %v", filename, err)
		}
		defer func(jsonFile *os.File) {
			_ = jsonFile.Close()
		}(jsonFile)

		byteValue, err := io.ReadAll(jsonFile)
		if err != nil {
			return fmt.Errorf("failed to unmarshal json exportData to map variable: %v", err)
		}

		exportAsString := fmt.Sprintf("%s", byteValue)
		numberOfDataSourcesExported := strings.Count(exportAsString, "\"Default_Outbound_Script\"")
		if numberOfDataSourcesExported != 1 {
			return fmt.Errorf("expected to find \"Default_Outbound_Script\" once in the exported content (actual %v). It is possible the Default Outbound Script data source is being exported more or less than once", numberOfDataSourcesExported)
		}

		return nil
	}
}

func loadJsonFileToMap(filename string) (map[string]interface{}, error) {
	var data map[string]interface{}

	jsonFile, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open export file at path %s: %v", filename, err)
	}
	defer func(jsonFile *os.File) {
		_ = jsonFile.Close()
	}(jsonFile)

	byteValue, _ := io.ReadAll(jsonFile)
	if err := json.Unmarshal(byteValue, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal json exportData to map variable: %v", err)
	}

	return data, nil
}

func testUserPromptAudioFileExport(filePath, resourceType, resourceLabel, exportDir, nameAttrRef string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		raw, err := getResourceDefinition(filePath, resourceType)
		if err != nil {
			return err
		}
		var r *json.RawMessage
		if err := json.Unmarshal(*raw[nameAttrRef], &r); err != nil {
			return err
		}

		var obj interface{}
		if err := json.Unmarshal(*r, &obj); err != nil {
			return err
		}

		// Collect each filename from resources list
		var fileNames []string
		if objMap, ok := obj.(map[string]interface{}); ok {
			if resourcesList, ok := objMap["resources"].([]interface{}); ok {
				for _, r := range resourcesList {
					if rMap, ok := r.(map[string]interface{}); ok {
						if fileNameStr, ok := rMap["filename"].(string); ok && fileNameStr != "" {
							fileNames = append(fileNames, fileNameStr)
						}
					}
				}
			}
		}

		// Check that file exists in export directory
		for _, filename := range fileNames {
			pathToWavFile := filepath.Join(exportDir, filename)
			if _, err := os.Stat(pathToWavFile); err != nil {
				return err
			}
		}

		return nil
	}
}

func TestAccResourceTfExportEnableDependsOn(t *testing.T) {
	var (
		exportTestDir            = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		exportResourceLabel      = "test-export2"
		contactListResourceLabel = "contact_list" + uuid.NewString()
		contactListName          = "terraform contact list" + uuid.NewString()
		outboundFlowFilePath     = filepath.Join(testrunner.RootDir, "examples/resources/genesyscloud_flow/outboundcall_flow_example.yaml")
		flowName                 = "testflowcxcase"
		flowResourceLabel        = "flow"
		wrapupcodeResourceLabel  = "wrapupcode"
	)
	defer os.RemoveAll(exportTestDir)

	config := fmt.Sprintf(`
data "genesyscloud_auth_division_home" "home" {}
`+`
resource "time_sleep" "wait_10_seconds" {
create_duration = "100s"
}
`) + GenerateOutboundCampaignBasicforFlowExport(
		contactListResourceLabel,
		outboundFlowFilePath,
		flowResourceLabel,
		flowName,
		"${data.genesyscloud_auth_division_home.home.name}",
		wrapupcodeResourceLabel,
		contactListName,
	) +
		generateTfExportByFlowDependsOnResources(
			exportResourceLabel,
			exportTestDir,
			util.TrueValue,
			[]string{
				strconv.Quote("genesyscloud_flow::" + flowName),
			},
			strconv.Quote("json"),
			util.FalseValue,
			util.TrueValue,
		)

	sanitizer := resourceExporter.NewSanitizerProvider()

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		ExternalProviders: map[string]resource.ExternalProvider{
			"time": {
				VersionConstraint: "0.10.0",
				Source:            "hashicorp/time",
			},
		},
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeTestCheckFunc(
					validateFlow("genesyscloud_flow."+flowResourceLabel, flowName),
					resource.TestCheckResourceAttr("genesyscloud_outbound_contact_list."+contactListResourceLabel, "name", contactListName),
					testDependentContactList(exportTestDir+"/"+defaultTfJSONFile, "genesyscloud_outbound_contact_list", sanitizer.S.SanitizeResourceBlockLabel(contactListName)),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

func TestAccResourceExporterFormat(t *testing.T) {
	t.Parallel()
	var (
		exportTestDir        = testrunner.GetTestTempPath(".terraform" + uuid.NewString())
		exportResourceLabel1 = "exportFormatTest-export1"
		jsonConfigFilePath   = filepath.Join(exportTestDir, defaultTfJSONFile)
		hclConfigFilePath    = filepath.Join(exportTestDir, defaultTfHCLFile)
	)

	defer os.RemoveAll(exportTestDir)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		CheckDestroy:      testVerifyExportsDestroyedFunc(exportTestDir),
		Steps: []resource.TestStep{
			{
				Config: generateTfExportResource_exportFormat(
					exportResourceLabel1,
					strconv.Quote("hcl_json"),
					[]string{"genesyscloud_journey_segment"},
					exportTestDir,
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("genesyscloud_tf_export."+exportResourceLabel1, "export_format", "hcl_json"),
					validateFileCreated(jsonConfigFilePath),
					validateFileCreated(hclConfigFilePath),
					validateConfigFile(jsonConfigFilePath),
					validateHCLConfigFile(hclConfigFilePath),
				),
			},
		},
	})
}

// TestAccResourceTfExportArchitectFlowExporterLegacyAndNew Exports a flow using the legacy exporter (creates a tfvars file but does not export flow config files)
// and then exports using the new archy exporter by setting use_legacy_architect_flow_exporter to false
func TestAccResourceTfExportArchitectFlowExporterLegacyAndNew(t *testing.T) {
	const (
		systemFlowName = "Default Voicemail Flow"
		systemFlowType = "VOICEMAIL"
		systemFlowId   = "de4c63f0-0be1-11ec-9a03-0242ac130003"
	)
	exportedSystemFlowFileName := architectFlow.BuildExportFileName(systemFlowName, systemFlowType, systemFlowId)
	exportResourceLabel := "export"
	exportTestDir := testrunner.GetTestTempPath(".terraform" + uuid.NewString())
	exportFullPath := ResourceType + "." + exportResourceLabel
	pathToFolderHoldingExportedFlows := filepath.Join(exportTestDir, architectFlow.ExportSubDirectoryName)

	defer func(path string) {
		if err := os.RemoveAll(path); err != nil {
			log.Printf("An error occured while removing directory '%s': %s", exportTestDir, err)
		}
	}(exportTestDir)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { util.TestAccPreCheck(t) },
		ProviderFactories: provider.GetProviderFactories(providerResources, providerDataSources),
		Steps: []resource.TestStep{
			{
				Config: generateTFExportResourceCustom(
					exportResourceLabel,
					exportTestDir,
					util.TrueValue,
					strconv.Quote("hcl"),
					util.NullValue, // use_legacy_architect_flow_exporter - should default to true
					[]string{
						strconv.Quote(architectFlow.ResourceType + "::" + systemFlowName),
					},
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(exportFullPath, "use_legacy_architect_flow_exporter", util.TrueValue),
					validateFileCreated(filepath.Join(exportTestDir, "terraform.tfvars")),
					validateFileNotCreated(filepath.Join(exportTestDir, architectFlow.ExportSubDirectoryName)),
				),
			},
			{
				Config: generateTFExportResourceCustom(
					exportResourceLabel,
					exportTestDir,
					util.TrueValue,
					strconv.Quote("hcl"),
					util.FalseValue, // use_legacy_architect_flow_exporter
					[]string{
						strconv.Quote(architectFlow.ResourceType + "::" + systemFlowName),
					},
				),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr(exportFullPath, "use_legacy_architect_flow_exporter", util.FalseValue),
					validateFileNotCreated(filepath.Join(exportTestDir, "terraform.tfvars")),
					validateFileCreated(pathToFolderHoldingExportedFlows),
					validateFileCreated(filepath.Join(pathToFolderHoldingExportedFlows, exportedSystemFlowFileName)),
				),
			},
		},
		CheckDestroy: testVerifyExportsDestroyedFunc(exportTestDir),
	})
}

func testUserExport(filePath, resourceType, resourceLabel string, expectedUser *UserExport) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		raw, err := getResourceDefinition(filePath, resourceType)
		if err != nil {
			return err
		}

		if raw[resourceLabel] == nil {
			return fmt.Errorf("expected a resource name for the resource type %s", resourceType)
		}

		var r *json.RawMessage
		if err := json.Unmarshal(*raw[resourceLabel], &r); err != nil {
			return err
		}

		exportedUser := &UserExport{}
		if err := json.Unmarshal(*r, exportedUser); err != nil {
			return err
		}

		if *exportedUser != *expectedUser {
			return fmt.Errorf("objects are not equal. Expected: %v. Got: %v", *expectedUser, *exportedUser)
		}

		return nil
	}
}

// testQueueExportEqual  Checks to see if the queues passed match the expected value
func testQueueExportEqual(filePath, resourceType, resourceLabel string, expectedQueue QueueExport) resource.TestCheckFunc {
	mccMutex.Lock()
	defer mccMutex.Unlock()
	return func(state *terraform.State) error {
		expectedQueue.OriginalResourceLabel = "" //Setting the resource label to be empty because it is not needed
		raw, err := getResourceDefinition(filePath, resourceType)
		if err != nil {
			return err
		}

		// Check if the raw data or label is nil
		if raw == nil {
			return fmt.Errorf("raw data is nil")
		}
		if _, ok := raw[resourceLabel]; !ok {
			return fmt.Errorf("resource label not found in raw data")
		}

		if _, ok := raw[resourceLabel]; !ok {
			return fmt.Errorf("failed to find resource %s in resource definition", resourceLabel)
		}

		var r *json.RawMessage
		if err := json.Unmarshal(*raw[resourceLabel], &r); err != nil {
			return err
		}

		// Check if r is nil
		if r == nil {
			return fmt.Errorf("unmarshaled raw message is nil")
		}

		exportedQueue := &QueueExport{}
		if err := json.Unmarshal(*r, exportedQueue); err != nil {
			return err
		}

		// Check if exportedQueue is nil
		if exportedQueue == nil {
			return fmt.Errorf("exportedQueue is nil after unmarshaling")
		}

		if *exportedQueue != expectedQueue {
			return fmt.Errorf("objects are not equal. Expected: %v. Got: %v", expectedQueue, *exportedQueue)
		}

		return nil
	}
}

// testDependentContactList tests to see if the dependent contactListResource for the flow is exported.
func testDependentContactList(filePath, resourceType, resourceLabel string) resource.TestCheckFunc {
	return func(state *terraform.State) error {

		_, err := os.ReadFile(filePath)
		if err != nil {
			fmt.Println("Error reading file:", err)
			return err
		}

		raw, err := getResourceDefinition(filePath, resourceType)
		if err != nil {
			return err
		}

		var r *json.RawMessage
		if err := json.Unmarshal(*raw[resourceLabel], &r); err != nil {
			return err
		}

		return nil
	}
}

// testQueueExportMatchesRegEx tests to see if all of the queues retrieved in the export match the regex passed into it.
func testQueueExportMatchesRegEx(filePath, resourceType, regEx string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		tfExport, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		var tfExportRaw map[string]*json.RawMessage
		if err := json.Unmarshal(tfExport, &tfExportRaw); err != nil {
			return err
		}

		var resourceRaw map[string]*json.RawMessage
		if err := json.Unmarshal(*tfExportRaw["resource"], &resourceRaw); err != nil {
			return err
		}

		var resources map[string]interface{}
		if json.Unmarshal(*resourceRaw[resourceType], &resources); err != nil {
			return err
		}

		for k := range resources {
			regEx := regexp.MustCompile(regEx)

			if !regEx.MatchString(k) {
				return fmt.Errorf("Resource %s::%s was found in the config file when it did not match the include regex: %s", resourceType, k, regEx)
			}
		}

		return nil
	}
}

// testWrapupcodeExportEqual  Checks to see if the wrapupcodes passed match the expected value
func testWrapupcodeExportEqual(filePath, resourceType, resourceLabel string, expectedWrapupcode WrapupcodeExport) resource.TestCheckFunc {
	mccMutex.Lock()
	defer mccMutex.Unlock()
	return func(state *terraform.State) error {
		expectedWrapupcode.OriginalResourceLabel = ""
		raw, err := getResourceDefinition(filePath, resourceType)
		if err != nil {
			return err
		}

		// Check if the raw data or name is nil
		if raw == nil {
			return fmt.Errorf("raw data is nil")
		}
		if _, ok := raw[resourceLabel]; !ok {
			return fmt.Errorf("resource name not found in raw data")
		}

		var r *json.RawMessage
		if err := json.Unmarshal(*raw[resourceLabel], &r); err != nil {
			return err
		}

		// Check if r is nil
		if r == nil {
			return fmt.Errorf("unmarshaled raw message is nil")
		}

		exportedWrapupcode := &WrapupcodeExport{}
		if err := json.Unmarshal(*r, exportedWrapupcode); err != nil {
			return err
		}

		// Check if exportedWrapupcode is nil
		if exportedWrapupcode == nil {
			return fmt.Errorf("exportedWrapupcode is nil after unmarshaling")
		}

		if *exportedWrapupcode != expectedWrapupcode {
			return fmt.Errorf("objects are not equal. Expected: %v. Got: %v", expectedWrapupcode, *exportedWrapupcode)
		}

		return nil
	}
}

func testQueueExportExcludesRegEx(filePath, resourceType, regEx string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		tfExport, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		var tfExportRaw map[string]*json.RawMessage
		if err := json.Unmarshal(tfExport, &tfExportRaw); err != nil {
			return err
		}

		var resourceRaw map[string]*json.RawMessage
		if err := json.Unmarshal(*tfExportRaw["resource"], &resourceRaw); err != nil {
			return err
		}

		var resources map[string]interface{}
		if json.Unmarshal(*resourceRaw[resourceType], &resources); err != nil {
			return err
		}

		for k := range resources {
			regEx := regexp.MustCompile(regEx)

			if regEx.MatchString(k) {
				return fmt.Errorf("Resource %s::%s was found in the config file when it should have been excluded by the regex: %s", resourceType, k, regEx)
			}
		}

		return nil
	}
}

func testTrunkBaseSettingsExport(filePath, resourceType string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		raw, err := getResourceDefinition(filePath, resourceType)
		if err != nil {
			return err
		}

		if len(raw) == 0 {
			return fmt.Errorf("expected several %v to be exported", resourceType)
		}

		return nil
	}
}

func getResourceDefinition(filePath, resourceType string) (map[string]*json.RawMessage, error) {
	tfExport, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var tfExportRaw map[string]*json.RawMessage
	if err := json.Unmarshal(tfExport, &tfExportRaw); err != nil {
		return nil, err
	}

	var resourceRaw map[string]*json.RawMessage
	if err := json.Unmarshal(*tfExportRaw["resource"], &resourceRaw); err != nil {
		return nil, err
	}

	if resourceRaw[resourceType] == nil {
		return nil, fmt.Errorf("%s not found in the config file", resourceType)
	}

	var r map[string]*json.RawMessage
	if err := json.Unmarshal(*resourceRaw[resourceType], &r); err != nil {
		return nil, err
	}

	return r, nil
}

// TestUnitTestForExportCycles creates a directed graph of exported resources to their references. Report any potential graph cycles in this test.
// Reference cycles can sometimes be broken by exporting a separate resource to update membership after the member
// and container resources are created/updated (see genesyscloud_user_roles).
func TestUnitTestForExportCycles(t *testing.T) {

	// Assumes exporting all resource types
	exporters := resourceExporter.GetResourceExporters()

	graph := simple.NewDirectedGraph()

	var resTypes []string
	for resType := range exporters {
		graph.AddNode(simple.Node(len(resTypes)))
		resTypes = append(resTypes, resType)
	}

	for i, resType := range resTypes {
		currNode := simple.Node(i)
		for attrName, refSettings := range exporters[resType].RefAttrs {
			if refSettings.RefType == resType {
				// Resources that can reference themselves are ignored
				// Cycles caused by self-refs are likely a misconfiguration
				// (e.g. two users that are each other's manager)
				continue
			}
			if exporters[resType].IsAttributeExcluded(attrName) {
				// This reference attribute will be excluded from the export
				continue
			}
			graph.SetEdge(simple.Edge{F: currNode, T: simple.Node(resNodeIndex(refSettings.RefType, resTypes))})
		}
	}

	cycles := topo.DirectedCyclesIn(graph)
	if len(cycles) > 0 {
		cycleResources := make([][]string, 0)
		for _, cycle := range cycles {
			cycleTemp := make([]string, len(cycle))
			for j, cycleNode := range cycle {
				if resTypes != nil {
					cycleTemp[j] = resTypes[cycleNode.ID()]
				}
			}
			if !isIgnoredReferenceCycle(cycleTemp) {
				cycleResources = append(cycleResources, cycleTemp)
			}
		}

		if len(cycleResources) > 0 {
			for _, cycleSlice := range cycleResources {
				if !isExcusedResourceExportCycle(cycleSlice) {
					t.Fatalf("Found the following potential reference cycles:\n %s", cycleResources)
				}
			}
		}
	}
}

// isExcusedResourceExportCycle makes an exception for the cyclic relationship between genesyscloud_telephony_providers_edges_site and
// genesyscloud_telephony_providers_edges_trunkbasesettings in the test TestUnitTestForExportCycles
// The genesyscloud_telephony_providers_edges_site_outbound_routes resource can be used as a workaround for any user
// having issues with this cycle
func isExcusedResourceExportCycle(cycle []string) bool {
	return slices.Contains(cycle, tbs.ResourceType) && slices.Contains(cycle, telephonyProvidersEdgesSite.ResourceType)
}

func generateExportResourceIncludeFilterWithEnableDepRes(
	resourceLabel,
	directory,
	includeStateFile,
	exportFormat,
	enableDepRes string,
	includeResources,
	dependsOn []string,
) string {
	return fmt.Sprintf(`
resource "genesyscloud_tf_export" "%s" {
	directory                    = "%s"
  	include_state_file           = %s
  	export_format                = %s
  	enable_dependency_resolution = %s
  	include_filter_resources     = [%s]
    depends_on = [%s]
}
`, resourceLabel, directory, includeStateFile, exportFormat, enableDepRes, strings.Join(includeResources, ", "), strings.Join(dependsOn, ", "))
}

func addContactsToContactList(state *terraform.State) error {
	outboundAPI := platformclientv2.NewOutboundApi()
	contactListResource := state.RootModule().Resources["genesyscloud_outbound_contact_list.contact_list"]
	if contactListResource == nil {
		return fmt.Errorf("genesyscloud_outbound_contact_list.contact_list contactListResource not found in state")
	}

	contactList, _, err := outboundAPI.GetOutboundContactlist(contactListResource.Primary.ID, false, false)
	if err != nil {
		return fmt.Errorf("genesyscloud_outbound_contact_list (%s) not available", contactListResource.Primary.ID)
	}
	contactsJSON := `[{
			"data": {
			  "First Name": "Asa",
			  "Last Name": "Acosta",
			  "Cell": "+13335554",
			  "Home": "3335552345"
			},
			"callable": true,
			"phoneNumberStatus": {}
		  },
		  {
			"data": {
			  "First Name": "Leonidas",
			  "Last Name": "Acosta",
			  "Cell": "4445551234",
			  "Home": "4445552345"
			},
			"callable": true,
			"phoneNumberStatus": {}
		  }]`
	var contacts []platformclientv2.Writabledialercontact
	err = json.Unmarshal([]byte(contactsJSON), &contacts)
	if err != nil {
		return fmt.Errorf("could not unmarshall JSON contacts to add to contact list")
	}
	_, _, err = outboundAPI.PostOutboundContactlistContacts(*contactList.Id, contacts, false, false, false)
	if err != nil {
		return fmt.Errorf("could not post contacts to contact list")
	}
	return nil
}

func isIgnoredReferenceCycle(cycle []string) bool {
	// Some cycles cannot be broken with a schema change and must be dealt with in the config
	// These cycles can be ignored by this test
	ignoredCycles := [][]string{
		// Email routes contain a ref to an inbound queue ID, and queues contain a ref to an outbound email route
		{"genesyscloud_routing_queue", "genesyscloud_routing_email_route", "genesyscloud_routing_queue"},
		{"genesyscloud_routing_email_route", "genesyscloud_routing_queue", "genesyscloud_routing_email_route"},
	}

	for _, ignored := range ignoredCycles {
		if util.StrArrayEquals(ignored, cycle) {
			return true
		}
	}
	return false
}

func resNodeIndex(resourceType string, resourceTypes []string) int64 {
	for i, resType := range resourceTypes {
		if resourceType == resType {
			return int64(i)
		}
	}
	return -1
}

func generateTfExportResource(
	resourceLabel string,
	directory string,
	includeState string,
	excludedAttributes string) string {
	return fmt.Sprintf(`resource "genesyscloud_tf_export" "%s" {
		directory = "%s"
		include_state_file = %s
		include_filter_resources = [
			"genesyscloud_architect_datatable",
			"genesyscloud_architect_datatable_row",
			//"genesyscloud_flow",
			"genesyscloud_flow_milestone",
			//"genesyscloud_flow_outcome",
			"genesyscloud_architect_ivr",
			"genesyscloud_architect_schedules",
			"genesyscloud_architect_schedulegroups",
			"genesyscloud_architect_user_prompt",
			"genesyscloud_auth_division",
			"genesyscloud_auth_role",
			"genesyscloud_employeeperformance_externalmetrics_definitions",
			"genesyscloud_group",
			"genesyscloud_group_roles",
			"genesyscloud_idp_adfs",
			"genesyscloud_idp_generic",
			"genesyscloud_idp_gsuite",
			"genesyscloud_idp_okta",
			"genesyscloud_idp_onelogin",
			"genesyscloud_idp_ping",
			"genesyscloud_idp_salesforce",
			"genesyscloud_integration",
			"genesyscloud_integration_action",
			"genesyscloud_integration_credential",
			"genesyscloud_location",
			"genesyscloud_oauth_client",
			"genesyscloud_outbound_settings",
			"genesyscloud_responsemanagement_library",
			"genesyscloud_routing_email_domain",
			"genesyscloud_routing_email_route",
			"genesyscloud_routing_language",
			//"genesyscloud_routing_queue",
			"genesyscloud_routing_settings",
			"genesyscloud_routing_skill",
			"genesyscloud_routing_utilization",
			"genesyscloud_routing_wrapupcode",
			"genesyscloud_telephony_providers_edges_did_pool",
			"genesyscloud_telephony_providers_edges_edge_group",
			"genesyscloud_telephony_providers_edges_phone",
			"genesyscloud_telephony_providers_edges_site",
			"genesyscloud_telephony_providers_edges_phonebasesettings",
			"genesyscloud_telephony_providers_edges_trunkbasesettings",
			"genesyscloud_telephony_providers_edges_trunk",
			//"genesyscloud_user_roles",
			"genesyscloud_webdeployments_configuration",
			"genesyscloud_webdeployments_deployment",
			"genesyscloud_knowledge_knowledgebase"
		]
		exclude_attributes = [%s]
	}
	`, resourceLabel, directory, includeState, excludedAttributes)
}

// generateTfExportResourceForCompress creates a resource to test compressed exported results
func generateTfExportResourceForCompress(
	resourceLabel string,
	directory string,
	includeState string,
	compressFlag string,
	includeResourcesFilter []string,
	excludedAttributes string) string {
	return fmt.Sprintf(`resource "genesyscloud_tf_export" "%s" {
		directory = "%s"
		include_state_file = %s
		compress=%s
		include_filter_resources = [%s]
		exclude_attributes = [%s]
	}
	`, resourceLabel, directory, includeState, compressFlag, strings.Join(includeResourcesFilter, ","), excludedAttributes)
}

func generateTfExportResourceMin(
	resourceLabel string,
	directory string,
	includeState string,
	excludedAttributes string) string {
	return fmt.Sprintf(`resource "genesyscloud_tf_export" "%s" {
		directory = "%s"
		include_state_file = %s
		resource_types = [
			"genesyscloud_routing_language",
			"genesyscloud_routing_settings",
			"genesyscloud_routing_skill",
			"genesyscloud_routing_utilization",
			"genesyscloud_routing_wrapupcode",
		]
		exclude_attributes = [%s]
	}
	`, resourceLabel, directory, includeState, excludedAttributes)
}

func generateTfExportByFilter(
	resourceLabel string,
	directory string,
	includeState string,
	resourceTypesFilter []string,
	excludedAttributes string,
	exportFormat string,
	logErrors string,
	dependencies []string) string {
	return fmt.Sprintf(`resource "genesyscloud_tf_export" "%s" {
		directory = "%s"
		include_state_file = %s
		resource_types = [%s]
		exclude_attributes = [%s]
		export_format = %s
		log_permission_errors = %s
		depends_on=[%s]
	}
	`, resourceLabel, directory, includeState, strings.Join(resourceTypesFilter, ","), excludedAttributes, exportFormat, logErrors, strings.Join(dependencies, ","))
}

func generateTfExportByIncludeFilterResources(
	resourceLabel string,
	directory string,
	includeState string,
	includeFilterResources []string,
	exportFormat string,
	splitByResource string,
	dependencies []string,
) string {
	return fmt.Sprintf(`resource "genesyscloud_tf_export" "%s" {
		directory = "%s"
		include_state_file = %s
		include_filter_resources = [%s]
		export_format = %s
		split_files_by_resource = %s
		depends_on = [%s]
	}
	`, resourceLabel, directory, includeState, strings.Join(includeFilterResources, ","), exportFormat, splitByResource, strings.Join(dependencies, ","))
}

func generateTfExportByFlowDependsOnResources(
	resourceLabel string,
	directory string,
	includeState string,
	includeFilterResources []string,
	exportFormat string,
	splitByResource string,
	dependsOn string,
) string {
	return fmt.Sprintf(`resource "genesyscloud_tf_export" "%s" {
		directory = "%s"
		include_state_file = %s
		include_filter_resources = [%s]
		export_format = %s
		split_files_by_resource = %s
		enable_dependency_resolution = %s
		depends_on = [time_sleep.wait_10_seconds]
	}
	`, resourceLabel, directory, includeState, strings.Join(includeFilterResources, ","), exportFormat, splitByResource, dependsOn)
}

func generateTfExportByExcludeFilterResources(
	resourceLabel string,
	directory string,
	includeState string,
	excludeFilterResources []string,
	exportFormat string,
	splitByResource string,
	dependencies []string,
) string {
	return fmt.Sprintf(`resource "genesyscloud_tf_export" "%s" {
		directory = "%s"
		include_state_file = %s
		exclude_filter_resources = [%s]
		log_permission_errors=true
		export_format = %s
		split_files_by_resource = %s
		depends_on=[%s]
	}
	`, resourceLabel, directory, includeState, strings.Join(excludeFilterResources, ","), exportFormat, splitByResource, strings.Join(dependencies, ","))
}

func generateTFExportResourceCustom(
	resourceLabel,
	directory,
	includeStateFile,
	exportFormat,
	useLegacyFlowExporter string,
	includeResources []string,
) string {
	return fmt.Sprintf(`
resource "%s" "%s" {
	directory                          = "%s"
	include_state_file                 = %s
	export_format                      = %s
	use_legacy_architect_flow_exporter = %s

	include_filter_resources = [%s]
}
`, ResourceType, resourceLabel, directory, includeStateFile, exportFormat, useLegacyFlowExporter, strings.Join(includeResources, "\n"))
}

func getExportedFileContents(filename string, result *string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		d, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("error reading file: %v\n", err)
		}
		*result = string(d)
		return nil
	}
}

func validateFileCreated(filename string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		_, err := os.Stat(filename)
		if err != nil {
			return fmt.Errorf("failed to find file '%s'. Error: %w", filename, err)
		}
		return nil
	}
}

func validateFileNotCreated(filename string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		_, err := os.Stat(filename)
		if err == nil {
			return fmt.Errorf("expected '%s' to not exist", filename)
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("unexpected error while verifying file '%s' does not exist: %w", filename, err)
		}
		return nil
	}
}

func validateCompressedCreated(filename string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		_, err := filepath.Glob(filename)
		if err != nil {
			return fmt.Errorf("Failed to find file")
		}
		return nil
	}
}

func deleteTestCompressedZip(exportPath string, zipFileName string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		dir, err := os.ReadDir(exportPath)
		if err != nil {
			return fmt.Errorf("Failed to read compressed zip %s", exportPath)
		}
		for _, d := range dir {
			os.RemoveAll(filepath.Join(exportPath, d.Name()))
		}
		files, err := filepath.Glob(zipFileName)

		if err != nil {
			return fmt.Errorf("Failed to get zip: %s", err)
		}
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				return fmt.Errorf("Failed to delete: %s", err)
			}
		}

		return nil
	}
}

func testVerifyExportsDestroyedFunc(exportTestDir string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		// Check config file deleted
		jsonConfigPath := filepath.Join(exportTestDir, defaultTfJSONFile)
		_, err := os.Stat(jsonConfigPath)
		if !os.IsNotExist(err) {
			return fmt.Errorf("Failed to delete JSON config file %s", jsonConfigPath)
		}

		// Check state file deleted
		statePath := filepath.Join(exportTestDir, defaultTfStateFile)
		_, err = os.Stat(statePath)
		if !os.IsNotExist(err) {
			return fmt.Errorf("Failed to delete state file %s", statePath)
		}
		return nil
	}
}

func validateEvaluationFormAttributes(resourceLabel string, form gcloud.EvaluationFormStruct) resource.TestCheckFunc {
	return resource.ComposeTestCheckFunc(
		resource.TestCheckResourceAttr("genesyscloud_quality_forms_evaluation."+resourceLabel, "name", resourceLabel),
		resource.TestCheckResourceAttr("genesyscloud_quality_forms_evaluation."+resourceLabel, "published", util.FalseValue),
		resource.TestCheckResourceAttr("genesyscloud_quality_forms_evaluation."+resourceLabel, "question_groups.0.name", form.QuestionGroups[0].Name),
		resource.TestCheckResourceAttr("genesyscloud_quality_forms_evaluation."+resourceLabel, "question_groups.0.weight", fmt.Sprintf("%v", form.QuestionGroups[0].Weight)),
		resource.TestCheckResourceAttr("genesyscloud_quality_forms_evaluation."+resourceLabel, "question_groups.0.questions.1.text", form.QuestionGroups[0].Questions[1].Text),
		resource.TestCheckResourceAttr("genesyscloud_quality_forms_evaluation."+resourceLabel, "question_groups.1.questions.0.answer_options.0.text", form.QuestionGroups[1].Questions[0].AnswerOptions[0].Text),
		resource.TestCheckResourceAttr("genesyscloud_quality_forms_evaluation."+resourceLabel, "question_groups.1.questions.0.answer_options.1.value", fmt.Sprintf("%v", form.QuestionGroups[1].Questions[0].AnswerOptions[1].Value)),
		resource.TestCheckResourceAttr("genesyscloud_quality_forms_evaluation."+resourceLabel, "question_groups.0.questions.1.visibility_condition.0.combining_operation", form.QuestionGroups[0].Questions[1].VisibilityCondition.CombiningOperation),
		resource.TestCheckResourceAttr("genesyscloud_quality_forms_evaluation."+resourceLabel, "question_groups.0.questions.1.visibility_condition.0.predicates.0", form.QuestionGroups[0].Questions[1].VisibilityCondition.Predicates[0]),
		resource.TestCheckResourceAttr("genesyscloud_quality_forms_evaluation."+resourceLabel, "question_groups.0.questions.1.visibility_condition.0.predicates.1", form.QuestionGroups[0].Questions[1].VisibilityCondition.Predicates[1]),
	)
}

// validateCompressedFile unzips and validates the exported resulted in the compressed folder
func validateCompressedFile(path string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		files, err := filepath.Glob(path)
		if err != nil {
			return err
		}
		for _, f := range files {
			reader, err := zip.OpenReader(f)
			if err != nil {
				return err
			}
			for _, file := range reader.File {
				err = validateCompressedConfigFiles(f, file)
				if err != nil {
					return err
				}
			}
		}
		return nil
	}
}

// validateCompressedConfigFiles validates the data inside the compressed json file
func validateCompressedConfigFiles(dirName string, file *zip.File) error {

	if file.FileInfo().Name() == defaultTfJSONFile {
		rc, _ := file.Open()

		buf := new(bytes.Buffer)
		buf.ReadFrom(rc)
		var data map[string]interface{}

		if err := json.Unmarshal(buf.Bytes(), &data); err != nil {
			return fmt.Errorf("failed to unmarshal json exportData to map variable: %v", err)
		}

		if _, ok := data["resource"]; !ok {
			return fmt.Errorf("config file missing resource attribute")
		}

		if _, ok := data["terraform"]; !ok {
			return fmt.Errorf("config file missing terraform attribute")
		}
		rc.Close()
		return nil
	}
	return nil
}

func validateConfigFile(path string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		result, err := loadJsonFileToMap(path)
		if err != nil {
			return err
		}

		if _, ok := result["resource"]; !ok {
			return fmt.Errorf("config file missing resource attribute")
		}

		if _, ok := result["terraform"]; !ok {
			return fmt.Errorf("config file missing terraform attribute")
		}
		return nil
	}
}

func validateHCLConfigFile(filename string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		parser := hclparse.NewParser()
		_, diag := parser.ParseHCLFile(filename)
		if diag.HasErrors() {
			return fmt.Errorf("Invalid HCL format: %v", diag)
		}
		return nil
	}
}

func validateMediaSettings(resourceLabel string, settingsAttr string, alertingTimeout string, slPercent string, slDurationMs string) resource.TestCheckFunc {
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr("genesyscloud_routing_queue."+resourceLabel, settingsAttr+".0.alerting_timeout_sec", alertingTimeout),
		resource.TestCheckResourceAttr("genesyscloud_routing_queue."+resourceLabel, settingsAttr+".0.service_level_percentage", slPercent),
		resource.TestCheckResourceAttr("genesyscloud_routing_queue."+resourceLabel, settingsAttr+".0.service_level_duration_ms", slDurationMs),
	)
}

func validateRoutingRules(resourceLabel string, ringNum int, operator string, threshold string, waitSec string) resource.TestCheckFunc {
	ringNumStr := strconv.Itoa(ringNum)
	return resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttr("genesyscloud_routing_queue."+resourceLabel, "routing_rules."+ringNumStr+".operator", operator),
		resource.TestCheckResourceAttr("genesyscloud_routing_queue."+resourceLabel, "routing_rules."+ringNumStr+".threshold", threshold),
		resource.TestCheckResourceAttr("genesyscloud_routing_queue."+resourceLabel, "routing_rules."+ringNumStr+".wait_seconds", waitSec),
	)
}

func buildQueueResources(queueExports []QueueExport) string {
	queueResourceDefinitions := ""
	for _, queueExport := range queueExports {
		queueResourceDefinitions = queueResourceDefinitions + routingQueue.GenerateRoutingQueueResource(
			queueExport.OriginalResourceLabel,
			queueExport.ExportedLabel,
			queueExport.Description,
			util.NullValue,                              // MANDATORY_TIMEOUT
			fmt.Sprintf("%v", queueExport.AcwTimeoutMs), // acw_timeout
			util.NullValue,                              // ALL
			util.NullValue,                              // auto_answer_only true
			util.NullValue,                              // No calling party name
			util.NullValue,                              // No calling party number
			util.NullValue,                              // enable_audio_monitoring false
			util.NullValue,                              // enable_manual_assignment false
			util.NullValue,                              //suppressCall_record_false
			util.NullValue,                              // enable_transcription false
			strconv.Quote("TimestampAndPriority"),
			util.NullValue,
			util.NullValue,
		)
	}

	return queueResourceDefinitions
}

func buildUserResources(userExports []UserExport) string {
	userResourceDefinitions := ""
	for _, userExport := range userExports {
		userResourceDefinitions = userResourceDefinitions + user.GenerateBasicUserResource(
			userExport.OriginalResourceLabel,
			userExport.Email,
			userExport.ExportedLabel,
		)
	}

	return userResourceDefinitions
}

func buildWrapupcodeResources(wrapupcodeExports []WrapupcodeExport, divisionId string, description string) string {
	wrapupcodeesourceDefinitions := ""
	for _, wrapupcodeExport := range wrapupcodeExports {
		wrapupcodeesourceDefinitions = wrapupcodeesourceDefinitions + routingWrapupcode.GenerateRoutingWrapupcodeResource(
			wrapupcodeExport.OriginalResourceLabel,
			wrapupcodeExport.Name,
			divisionId,
			description,
		)
	}

	return wrapupcodeesourceDefinitions
}

// Returns random string. Helpful for regex export testing as unique prefix or postfix
func randString(length int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	letters := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")

	s := make([]rune, length)
	for i := range s {
		s[i] = letters[r.Intn(len(letters))]
	}

	return string(s)
}

func GenerateOutboundCampaignBasicforFlowExport(
	contactListResourceLabel string,
	outboundFlowFilePath string,
	flowResourceLabel string,
	flowName string,
	divisionName,
	wrapupcodeResourceLabel string,
	contactListName string) string {
	referencedResources := GenerateReferencedResourcesForOutboundCampaignTests(
		contactListResourceLabel,
		outboundFlowFilePath,
		flowResourceLabel,
		flowName,
		divisionName,
		wrapupcodeResourceLabel,
		contactListName,
	)
	return fmt.Sprintf(`
%s
`, referencedResources)
}

func GenerateReferencedResourcesForOutboundCampaignTests(
	contactListResourceLabel string,
	outboundFlowFilePath string,
	flowResourceLabel string,
	flowName string,
	divisionName string,
	wrapUpCodeResourceLabel string,
	contactListName string,
) string {
	var (
		contactList             string
		callAnalysisResponseSet string
		divResourceLabel        = "test-division"
		divName                 = "terraform-" + uuid.NewString()
		description             = "Terraform wrapup code description"
	)
	if contactListResourceLabel != "" {
		contactList = obContactList.GenerateOutboundContactList(
			contactListResourceLabel,
			contactListName,
			util.NullValue,
			strconv.Quote("Cell"),
			[]string{strconv.Quote("Cell")},
			[]string{strconv.Quote("Cell"), strconv.Quote("Home"), strconv.Quote("zipcode")},
			util.FalseValue,
			util.NullValue,
			util.NullValue,
			obContactList.GeneratePhoneColumnsBlock("Cell", "cell", strconv.Quote("Cell")),
			obContactList.GeneratePhoneColumnsBlock("Home", "home", strconv.Quote("Home")))
	}

	callAnalysisResponseSet = authDivision.GenerateAuthDivisionBasic(divResourceLabel, divName) + routingWrapupcode.GenerateRoutingWrapupcodeResource(
		wrapUpCodeResourceLabel,
		"wrapupcode "+uuid.NewString(),
		"genesyscloud_auth_division."+divResourceLabel+".id",
		description,
	) + architectFlow.GenerateFlowResource(
		flowResourceLabel,
		outboundFlowFilePath,
		"",
		false,
		util.GenerateSubstitutionsMap(map[string]string{
			"flow_name":          flowName,
			"home_division_name": divisionName,
			"contact_list_name":  "${genesyscloud_outbound_contact_list." + contactListResourceLabel + ".name}",
			"wrapup_code_name":   "${genesyscloud_routing_wrapupcode." + wrapUpCodeResourceLabel + ".name}",
		}),
	)

	return fmt.Sprintf(`
			%s
			%s
		`, contactList, callAnalysisResponseSet)
}

// Check if flow is published, then check if flow name and type are correct
func validateFlow(flowResourcePath, flowName string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		flowResource, ok := state.RootModule().Resources[flowResourcePath]
		if !ok {
			return fmt.Errorf("failed to find flow %s in state", flowResourcePath)
		}
		flowID := flowResource.Primary.ID
		architectAPI := platformclientv2.NewArchitectApi()

		log.Printf("Reading flow %s", flowID)
		flow, _, err := architectAPI.GetFlow(flowID, false)
		if err != nil {
			return fmt.Errorf("unexpected error: %s", err)
		}

		if flow == nil {
			return fmt.Errorf("Flow (%s) not found. ", flowID)
		}

		if *flow.Name != flowName {
			return fmt.Errorf("returned flow (%s) has incorrect name. Expect: %s, Actual: %s", flowID, flowName, *flow.Name)
		}

		return nil
	}
}

func generateTfExportResource_exportFormat(
	exportResourceLabel1 string,
	exportFormat string,
	includeResources []string,
	path string) string {

	includeResourceStr := "null"
	if includeResources != nil {
		includeResourceStr = "[" + strings.Join(formatStringArray(includeResources), ",") + "]"
	}

	return fmt.Sprintf(`resource "genesyscloud_tf_export" "%s" {
        export_format = "%s"
        include_filter_resources = %s
        directory = "%s"
    }
    `, exportResourceLabel1, exportFormat, includeResourceStr, path)
}

func formatStringArray(arr []string) []string {
	quotedStrings := make([]string, len(arr))
	for i, s := range arr {
		quotedStrings[i] = fmt.Sprintf(`"%s"`, s)
	}
	return quotedStrings
}
