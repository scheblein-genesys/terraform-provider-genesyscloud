package conversations_messaging_supportedcontent_default

import (
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

	"terraform-provider-genesyscloud/genesyscloud/provider"
	registrar "terraform-provider-genesyscloud/genesyscloud/resource_register"
)

/*
resource_genesycloud_conversations_messaging_supportedcontent_default_schema.go holds four functions within it:

1.  The registration code that registers the Datasource, Resource and Exporter for the package.
2.  The resource schema definitions for the conversations_messaging_supportedcontent_default resource.
3.  The datasource schema definitions for the conversations_messaging_supportedcontent_default datasource.
4.  The resource exporter configuration for the conversations_messaging_supportedcontent_default exporter.
*/
const resourceName = "genesyscloud_conversations_messaging_supportedcontent_default"

// SetRegistrar registers all of the resources, datasources and exporters in the package
func SetRegistrar(regInstance registrar.Registrar) {
	regInstance.RegisterResource(resourceName, ResourceConversationsMessagingSupportedcontentDefault())
}

// ResourceConversationsMessagingSupportedcontentDefault registers the genesyscloud_conversations_messaging_supportedcontent_default resource with Terraform
func ResourceConversationsMessagingSupportedcontentDefault() *schema.Resource {
	return &schema.Resource{
		Description: `Genesys Cloud conversations messaging supportedcontent default`,

		CreateContext: provider.CreateWithPooledClient(createConversationsMessagingSupportedcontentDefault),
		ReadContext:   provider.ReadWithPooledClient(readConversationsMessagingSupportedcontentDefault),
		UpdateContext: provider.UpdateWithPooledClient(updateConversationsMessagingSupportedcontentDefault),
		DeleteContext: provider.DeleteWithPooledClient(deleteConversationsMessagingSupportedcontentDefault),
		Importer: &schema.ResourceImporter{
			StateContext: schema.ImportStatePassthroughContext,
		},
		SchemaVersion: 1,
		Schema: map[string]*schema.Schema{
			`content_id`: {
				Description: `The SupportedContent unique identifier associated with this integration`,
				Required:    true,
				Type:        schema.TypeString,
			},
		},
	}
}
