package architect_flow

import (
	"context"
	"fmt"
	"github.com/mypurecloud/terraform-provider-genesyscloud/genesyscloud/provider"
	"github.com/mypurecloud/terraform-provider-genesyscloud/genesyscloud/util"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/retry"
	"github.com/mypurecloud/platform-client-sdk-go/v150/platformclientv2"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func dataSourceFlowRead(ctx context.Context, d *schema.ResourceData, m interface{}) diag.Diagnostics {
	var (
		sdkConfig = m.(*provider.ProviderMeta).ClientConfig
		p         = getArchitectFlowProxy(sdkConfig)
		response  *platformclientv2.APIResponse

		name       = d.Get("name").(string)
		varType, _ = d.Get("type").(string)
	)

	varType = strings.ToLower(varType)

	diagErr := util.WithRetries(ctx, 5*time.Second, func() *retry.RetryError {
		flowId, resp, retryable, err := p.getFlowIdByNameAndType(ctx, name, varType)
		if err != nil {
			response = resp
			if retryable {
				return retry.RetryableError(err)
			}
			return retry.NonRetryableError(err)
		}
		d.SetId(flowId)
		return nil
	})

	if diagErr != nil {
		msg := fmt.Sprintf("error retrieving ID of flow '%s' | error: %v", name, diagErr)
		return util.BuildAPIDiagnosticError(ResourceType, msg, response)
	}

	return nil
}
