package genesyscloud

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/mypurecloud/platform-client-sdk-go/v80/platformclientv2"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestAccResourceFlow(t *testing.T) {
	var (
		flowResource1 = "test_flow1"
		//flowResource2 = "test_flow2"
		flowName1 = "Terraform Flow Test-" + uuid.NewString()
		flowName2 = "Terraform Flow Test-" + uuid.NewString()
		flowType1 = "INBOUNDCALL"
		//flowType2     = "INBOUNDEMAIL"
		filePath1 = "../examples/resources/genesyscloud_flow/inboundcall_flow_example.yaml"
		filePath2 = "../examples/resources/genesyscloud_flow/inboundcall_flow_example2.yaml"
		//filePath3     = "../examples/resources/genesyscloud_flow/inboundcall_flow_example3.yaml"

		inboundcallConfig1 = fmt.Sprintf("inboundCall:\n  name: %s\n  defaultLanguage: en-us\n  startUpRef: ./menus/menu[mainMenu]\n  initialGreeting:\n    tts: Archy says hi!!!\n  menus:\n    - menu:\n        name: Main Menu\n        audio:\n          tts: You are at the Main Menu, press 9 to disconnect.\n        refId: mainMenu\n        choices:\n          - menuDisconnect:\n              name: Disconnect\n              dtmf: digit_9", flowName1)
		inboundcallConfig2 = fmt.Sprintf("inboundCall:\n  name: %s\n  defaultLanguage: en-us\n  startUpRef: ./menus/menu[mainMenu]\n  initialGreeting:\n    tts: Archy says hi!!!!!\n  menus:\n    - menu:\n        name: Main Menu\n        audio:\n          tts: You are at the Main Menu, press 9 to disconnect.\n        refId: mainMenu\n        choices:\n          - menuDisconnect:\n              name: Disconnect\n              dtmf: digit_9", flowName2)
	)

	var homeDivisionName string
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: providerFactories,
		Steps: []resource.TestStep{
			{
				Config: "data \"genesyscloud_auth_division_home\" \"home\" {}",
				Check: resource.ComposeTestCheckFunc(
					getHomeDivisionName("data.genesyscloud_auth_division_home.home", &homeDivisionName),
				),
			},
		},
	})

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: providerFactories,
		Steps: []resource.TestStep{
			{
				// Create flow
				Config: generateFlowResourceForceUnlock(
					flowResource1,
					true,
					filePath1,
					inboundcallConfig1,
				),
				Check: resource.ComposeTestCheckFunc(
					validateFlow("genesyscloud_flow."+flowResource1, flowName1, flowType1),
				),
			},
			{
				// Update flow with name
				Config: generateFlowResourceForceUnlock(
					flowResource1,
					false,
					filePath2,
					inboundcallConfig2,
				),
				Check: resource.ComposeTestCheckFunc(
					validateFlow("genesyscloud_flow."+flowResource1, flowName2, flowType1),
				),
			},
		},
		CheckDestroy: testVerifyFlowDestroyed,
	})
}

func TestAccResourceFlowURL(t *testing.T) {
	var (
		flowResource1 = "test_flow1"
		filePath1     = "http://localhost:8101/inboundcall_flow_example.yaml"
		filePath2     = "http://localhost:8101/inboundcall_flow_example2.yaml"
	)

	httpServerExitDone := &sync.WaitGroup{}
	httpServerExitDone.Add(1)
	srv := startHttpServer(httpServerExitDone, "../examples/resources/genesyscloud_flow", "8101")

	var homeDivisionName string
	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: providerFactories,
		Steps: []resource.TestStep{
			{
				Config: "data \"genesyscloud_auth_division_home\" \"home\" {}",
				Check: resource.ComposeTestCheckFunc(
					getHomeDivisionName("data.genesyscloud_auth_division_home.home", &homeDivisionName),
				),
			},
		},
	})

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: providerFactories,
		Steps: []resource.TestStep{
			{
				// Create flow
				Config: generateFlowResourceURL(
					flowResource1,
					filePath1,
				),
			},
			{
				// Update flow with name
				Config: generateFlowResourceURL(
					flowResource1,
					filePath2,
				),
			},
			{
				// Import/Read
				ResourceName:            "genesyscloud_flow." + flowResource1,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"filepath", "file_content_hash"},
			},
		},
		CheckDestroy: testVerifyFlowDestroyed,
	})
	if err := srv.Shutdown(context.TODO()); err != nil {
		log.Println("Error shutting down server:", err)
	}

	httpServerExitDone.Wait()
}

func TestAccResourceFlowSubstitutions(t *testing.T) {
	var (
		flowResource1 = "test_flow1"
		flowName1     = "Terraform Flow Test-" + uuid.NewString()
		flowName2     = "Terraform Flow Test-" + uuid.NewString()
		filePath1     = "../examples/resources/genesyscloud_flow/inboundcall_flow_example_substitutions.yaml"
	)

	resource.Test(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: providerFactories,
		Steps: []resource.TestStep{
			{
				// Create flow
				Config: generateFlowResource(
					flowResource1,
					filePath1,
					"",
					generateFlowSubstitutions(map[string]string{
						"flow_name":            flowName1,
						"default_language":     "en-us",
						"greeting":             "Archy says hi!!!",
						"menu_disconnect_name": "Disconnect",
					}),
				),
				Check: resource.ComposeTestCheckFunc(
					validateFlow("genesyscloud_flow."+flowResource1, flowName1, "INBOUNDCALL"),
				),
			},
			{
				// Update
				Config: generateFlowResource(
					flowResource1,
					filePath1,
					"",
					generateFlowSubstitutions(map[string]string{
						"flow_name":            flowName2,
						"default_language":     "en-us",
						"greeting":             "Archy says hi!!!",
						"menu_disconnect_name": "Disconnect",
					}),
				),
				Check: resource.ComposeTestCheckFunc(
					validateFlow("genesyscloud_flow."+flowResource1, flowName2, "INBOUNDCALL"),
				),
			},
		},
		CheckDestroy: testVerifyFlowDestroyed,
	})
}

func generateFlowSubstitutions(substitutions map[string]string) string {
	var substitutionsStr string
	for k, v := range substitutions {
		substitutionsStr += fmt.Sprintf("\t%s = \"%s\"\n", k, v)
	}
	return fmt.Sprintf(`substitutions = {
%s}`, substitutionsStr)
}

func generateFlowResourceForceUnlock(resourceID string, forceUnlock bool, filepath, filecontent string, substitutions ...string) string {
	if filecontent != "" {
		updateFile(filepath, filecontent)
	}

	return fmt.Sprintf(`resource "genesyscloud_flow" "%s" {
		force_unlock = %v
        filepath = %s
		%s
	}
	`, resourceID, forceUnlock, strconv.Quote(filepath), strings.Join(substitutions, "\n"))
}

func generateFlowResource(resourceID, filepath, filecontent string, substitutions ...string) string {
	if filecontent != "" {
		updateFile(filepath, filecontent)
	}

	return fmt.Sprintf(`resource "genesyscloud_flow" "%s" {
        filepath = %s
		%s
	}
	`, resourceID, strconv.Quote(filepath), strings.Join(substitutions, "\n"))
}

func generateFlowResourceURL(resourceID, filepath string) string {
	return fmt.Sprintf(`resource "genesyscloud_flow" "%s" {
        filepath = %s
	}
	`, resourceID, strconv.Quote(filepath))
}

func updateFile(filepath, content string) {
	file, err := os.OpenFile(filepath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666)

	if err != nil {
		log.Println(err)
		return
	}
	defer file.Close()

	file.WriteString(content)
}

// Check if flow is published, then check if flow name and type are correct
func validateFlow(flowResourceName, name, flowType string) resource.TestCheckFunc {
	return func(state *terraform.State) error {
		flowResource, ok := state.RootModule().Resources[flowResourceName]
		if !ok {
			return fmt.Errorf("Failed to find flow %s in state", flowResourceName)
		}
		flowID := flowResource.Primary.ID
		architectAPI := platformclientv2.NewArchitectApi()

		flow, _, err := architectAPI.GetFlow(flowID, false)

		if err != nil {
			return fmt.Errorf("Unexpected error: %s", err)
		}

		if flow == nil {
			return fmt.Errorf("Flow (%s) not found. ", flowID)
		}

		if *flow.Name != name {
			return fmt.Errorf("Returned flow (%s) has incorrect name. Expect: %s, Actual: %s", flowID, name, *flow.Name)
		}

		if *flow.VarType != flowType {
			return fmt.Errorf("Returned flow (%s) has incorrect type. Expect: %s, Actual: %s", flowID, flowType, *flow.VarType)
		}

		return nil
	}
}

func testVerifyFlowDestroyed(state *terraform.State) error {
	architectAPI := platformclientv2.NewArchitectApi()
	for _, rs := range state.RootModule().Resources {
		if rs.Type != "genesyscloud_flow" {
			continue
		}

		flow, resp, err := architectAPI.GetFlow(rs.Primary.ID, false)
		if flow != nil {
			return fmt.Errorf("Flow (%s) still exists", rs.Primary.ID)
		} else if resp != nil && resp.StatusCode == 410 {
			// Flow not found as expected
			continue
		} else {
			// Unexpected error
			return fmt.Errorf("Unexpected error: %s", err)
		}
	}
	// Success. All Flows destroyed
	return nil
}
