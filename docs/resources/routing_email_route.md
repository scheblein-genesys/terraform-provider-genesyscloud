---
page_title: "genesyscloud_routing_email_route Resource - terraform-provider-genesyscloud"
subcategory: ""
description: |-
  Genesys Cloud Routing Email Domain Route
---
# genesyscloud_routing_email_route (Resource)

Genesys Cloud Routing Email Domain Route

## API Usage
The following Genesys Cloud APIs are used by this resource. Ensure your OAuth Client has been granted the necessary scopes and permissions to perform these operations:

* [GET /api/v2/routing/email/domains/{domainName}/routes](https://developer.mypurecloud.com/api/rest/v2/routing/#get-api-v2-routing-email-domains--domainName--routes)
* [POST /api/v2/routing/email/domains/{domainName}/routes](https://developer.mypurecloud.com/api/rest/v2/routing/#post-api-v2-routing-email-domains--domainName--routes)
* [GET /api/v2/routing/email/domains/{domainName}/routes/{routeId}](https://developer.mypurecloud.com/api/rest/v2/routing/#get-api-v2-routing-email-domains--domainName--routes--routeId-)
* [PUT /api/v2/routing/email/domains/{domainName}/routes/{routeId}](https://developer.mypurecloud.com/api/rest/v2/routing/#put-api-v2-routing-email-domains--domainName--routes--routeId-)
* [DELETE /api/v2/routing/email/domains/{domainName}/routes/{routeId}](https://developer.mypurecloud.com/api/rest/v2/routing/#delete-api-v2-routing-email-domains--domainName--routes--routeId-)

## Example Usage

```terraform
resource "genesyscloud_routing_email_route" "support-route" {
  domain_id    = "example.domain.com"
  pattern      = "support"
  from_name    = "Example Support"
  from_email   = "examplesupport@example.domain.com"
  queue_id     = genesyscloud_routing_queue.example-queue.id
  priority     = 5
  skill_ids    = [genesyscloud_routing_skill.support.id]
  language_id  = genesyscloud_routing_language.english.id
  flow_id      = data.genesyscloud_flow.flow.id
  spam_flow_id = data.genesyscloud_flow.spam_flow.id
  reply_email_address {
    domain_id = "example.domain.com"
    route_id  = genesyscloud_routing_email_route.example.id
  }
  auto_bcc {
    name  = "Example Support"
    email = "support@example.domain.com"
  }
}
```

<!-- schema generated by tfplugindocs -->
## Schema

### Required

- `domain_id` (String) ID of the routing domain such as: 'example.com'. Changing the domain_id attribute will cause the email_route object to be dropped and recreated with a new ID.
- `from_name` (String) The sender name to use for outgoing replies.
- `pattern` (String) The search pattern that the mailbox name should match.

### Optional

- `allow_multiple_actions` (Boolean) Control if multiple actions are allowed on this route. When true the disconnect has to be done manually. When false a conversation will be disconnected by the system after every action.
- `auto_bcc` (Block Set) The recipients that should be automatically blind copied on outbound emails associated with this route. This should not be set if reply_email_address is specified. (see [below for nested schema](#nestedblock--auto_bcc))
- `flow_id` (String) The flow to use for processing the email. This should not be set if a queue_id is specified.
- `from_email` (String) The sender email to use for outgoing replies. This should not be set if reply_email_address is specified.
- `history_inclusion` (String) The configuration to indicate how the history of a conversation has to be included in a draft. Defaults to `Optional`.
- `language_id` (String) The language to use for routing.
- `priority` (Number) The priority to use for routing.
- `queue_id` (String) The queue to route the emails to. This should not be set if a flow_id is specified.
- `reply_email_address` (Block List, Max: 1) The route to use for email replies. This should not be set if from_email or auto_bcc are specified. (see [below for nested schema](#nestedblock--reply_email_address))
- `skill_ids` (Set of String) The skills to use for routing.
- `spam_flow_id` (String) The flow to use for processing inbound emails that have been marked as spam.

### Read-Only

- `id` (String) The ID of this resource.

<a id="nestedblock--auto_bcc"></a>
### Nested Schema for `auto_bcc`

Required:

- `email` (String) Email address.

Optional:

- `name` (String) Name associated with the email.


<a id="nestedblock--reply_email_address"></a>
### Nested Schema for `reply_email_address`

Required:

- `domain_id` (String) Domain of the route.

Optional:

- `route_id` (String) ID of the route.
- `self_reference_route` (Boolean) Use this route as the reply email address. If true you will use the route id for this resource as the reply and you
							              can not set a route. If you set this value to false (or leave the attribute off)you must set a route id. Defaults to `false`.

