package knowledge_document

import (
	"context"
	rc "terraform-provider-genesyscloud/genesyscloud/resource_cache"

	"github.com/mypurecloud/platform-client-sdk-go/v133/platformclientv2"
)

var internalProxy *knowledgeDocumentProxy

type getKnowledgeKnowledgebaseCategoryFunc func(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, categoryId string) (*platformclientv2.Categoryresponse, *platformclientv2.APIResponse, error)
type getKnowledgeKnowledgebaseCategoriesFunc func(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, pageSize int, categoryName string) (*platformclientv2.Categoryresponselisting, *platformclientv2.APIResponse, error)
type getKnowledgeKnowledgebaseLabelsFunc func(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, pageSize int, labelName string) (*[]platformclientv2.Labellisting, *platformclientv2.APIResponse, error)
type getKnowledgeKnowledgebaseLabelFunc func(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, labelId string) (*platformclientv2.Labelresponse, *platformclientv2.APIResponse, error)
type getKnowledgeKnowledgebaseDocumentFunc func(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, documentId string, expand []string, state string) (*platformclientv2.Knowledgedocumentresponse, *platformclientv2.APIResponse, error)
type getAllKnowledgebaseEntitiesFunc func(ctx context.Context, p *knowledgeDocumentProxy, published bool) (*[]platformclientv2.Knowledgebase, *platformclientv2.APIResponse, error)
type createKnowledgeKnowledgebaseDocumentFunc func(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, body *platformclientv2.Knowledgedocumentreq) (*platformclientv2.Knowledgedocumentresponse, *platformclientv2.APIResponse, error)
type createKnowledgebaseDocumentVersionsFunc func(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, documentId string, body platformclientv2.Knowledgedocumentversion) (*platformclientv2.Knowledgedocumentversion, *platformclientv2.APIResponse, error)
type deleteKnowledgeKnowledgebaseDocumentFunc func(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, documentId string) (*platformclientv2.APIResponse, error)
type updateKnowledgeKnowledgebaseDocumentFunc func(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, documentId string, body *platformclientv2.Knowledgedocumentreq) (*platformclientv2.Knowledgedocumentresponse, *platformclientv2.APIResponse, error)

type knowledgeDocumentProxy struct {
	clientConfig                             *platformclientv2.Configuration
	knowledgeApi                             *platformclientv2.KnowledgeApi
	getKnowledgeKnowledgebaseCategoryAttr    getKnowledgeKnowledgebaseCategoryFunc
	getKnowledgeKnowledgebaseCategoriesAttr  getKnowledgeKnowledgebaseCategoriesFunc
	getKnowledgeKnowledgebaseLabelsAttr      getKnowledgeKnowledgebaseLabelsFunc
	getKnowledgeKnowledgebaseLabelAttr       getKnowledgeKnowledgebaseLabelFunc
	getKnowledgeKnowledgebaseDocumentAttr    getKnowledgeKnowledgebaseDocumentFunc
	getAllKnowledgebaseEntitiesAttr          getAllKnowledgebaseEntitiesFunc
	createKnowledgeKnowledgebaseDocumentAttr createKnowledgeKnowledgebaseDocumentFunc
	deleteKnowledgeKnowledgebaseDocumentAttr deleteKnowledgeKnowledgebaseDocumentFunc
	updateKnowledgeKnowledgebaseDocumentAttr updateKnowledgeKnowledgebaseDocumentFunc
	knowledgeDocumentCache                   rc.CacheInterface[platformclientv2.Knowledgedocumentresponse]
}

func newKnowledgeDocumentProxy(clientConfig *platformclientv2.Configuration) *knowledgeDocumentProxy {
	api := platformclientv2.NewKnowledgeApiWithConfig(clientConfig)
	knowledgeDocumentCache := rc.NewResourceCache[platformclientv2.Knowledgedocumentresponse]()
	return &knowledgeDocumentProxy{
		clientConfig:                             clientConfig,
		knowledgeApi:                             api,
		getKnowledgeKnowledgebaseCategoryAttr:    getKnowledgeKnowledgebaseCategoryFn,
		getKnowledgeKnowledgebaseCategoriesAttr:  getKnowledgeKnowledgebaseCategoriesFn,
		getKnowledgeKnowledgebaseLabelsAttr:      getKnowledgeKnowledgebaseLabelsFn,
		getKnowledgeKnowledgebaseLabelAttr:       getKnowledgeKnowledgebaseLabelFn,
		getKnowledgeKnowledgebaseDocumentAttr:    getKnowledgeKnowledgebaseDocumentFn,
		getAllKnowledgebaseEntitiesAttr:          getAllKnowledgebaseEntitiesFn,
		createKnowledgeKnowledgebaseDocumentAttr: createKnowledgeKnowledgebaseDocumentFn,
		deleteKnowledgeKnowledgebaseDocumentAttr: deleteKnowledgeKnowledgebaseDocumentFn,
		updateKnowledgeKnowledgebaseDocumentAttr: updateKnowledgeKnowledgebaseDocumentFn,
		knowledgeDocumentCache:                   knowledgeDocumentCache,
	}
}

func getKnowledgeDocumentProxy(clientConfig *platformclientv2.Configuration) *knowledgeDocumentProxy {
	if internalProxy == nil {
		internalProxy = newKnowledgeDocumentProxy(clientConfig)
	}

	return internalProxy
}

func (p *knowledgeDocumentProxy) getKnowledgeKnowledgebaseCategory(ctx context.Context, knowledgeBaseId string, categoryId string) (*platformclientv2.Categoryresponse, *platformclientv2.APIResponse, error) {
	return p.getKnowledgeKnowledgebaseCategoryAttr(ctx, p, knowledgeBaseId, categoryId)
}

func (p *knowledgeDocumentProxy) getKnowledgeKnowledgebaseCategories(ctx context.Context, knowledgeBaseId string, pageSize int, categoryName string) (*platformclientv2.Categoryresponselisting, *platformclientv2.APIResponse, error) {
	return p.getKnowledgeKnowledgebaseCategoriesAttr(ctx, p, knowledgeBaseId, pageSize, categoryName)
}

func (p *knowledgeDocumentProxy) getKnowledgeKnowledgebaseLabels(ctx context.Context, knowledgeBaseId string, pageSize int, labelName string) (*[]platformclientv2.Labellisting, *platformclientv2.APIResponse, error) {
	return p.getKnowledgeKnowledgebaseLabelsAttr(ctx, p, knowledgeBaseId, pageSize, labelName)
}

func (p *knowledgeDocumentProxy) getKnowledgeKnowledgebaseLabel(ctx context.Context, knowledgeBaseId string, labelId string) (*platformclientv2.Labelresponse, *platformclientv2.APIResponse, error) {
	return p.getKnowledgeKnowledgebaseLabelAttr(ctx, p, knowledgeBaseId, labelId)
}

func (p *knowledgeDocumentProxy) getKnowledgeKnowledgebaseDocument(ctx context.Context, knowledgeBaseId string, documentId string, expand []string, state string) (*platformclientv2.Knowledgedocumentresponse, *platformclientv2.APIResponse, error) {
	return p.getKnowledgeKnowledgebaseDocumentAttr(ctx, p, knowledgeBaseId, documentId, expand, state)
}

func (p *knowledgeDocumentProxy) getAllKnowledgebaseEntities(ctx context.Context, published bool) (*[]platformclientv2.Knowledgebase, *platformclientv2.APIResponse, error) {
	return p.getAllKnowledgebaseEntitiesAttr(ctx, p, published)
}

func (p *knowledgeDocumentProxy) createKnowledgeKnowledgebaseDocument(ctx context.Context, knowledgeBaseId string, body *platformclientv2.Knowledgedocumentreq) (*platformclientv2.Knowledgedocumentresponse, *platformclientv2.APIResponse, error) {
	return p.createKnowledgeKnowledgebaseDocumentAttr(ctx, p, knowledgeBaseId, body)
}

func (p *knowledgeDocumentProxy) deleteKnowledgeKnowledgebaseDocument(ctx context.Context, knowledgeBaseId string, documentId string) (*platformclientv2.APIResponse, error) {
	return p.deleteKnowledgeKnowledgebaseDocumentAttr(ctx, p, knowledgeBaseId, documentId)
}

func (p *knowledgeDocumentProxy) updateKnowledgeKnowledgebaseDocument(ctx context.Context, knowledgeBaseId string, documentId string, body *platformclientv2.Knowledgedocumentreq) (*platformclientv2.Knowledgedocumentresponse, *platformclientv2.APIResponse, error) {
	return p.updateKnowledgeKnowledgebaseDocumentAttr(ctx, p, knowledgeBaseId, documentId, body)
}

func getKnowledgeKnowledgebaseCategoryFn(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, categoryId string) (*platformclientv2.Categoryresponse, *platformclientv2.APIResponse, error) {
	return p.knowledgeApi.GetKnowledgeKnowledgebaseCategory(knowledgeBaseId, categoryId)
}

func getKnowledgeKnowledgebaseCategoriesFn(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, pageSize int, categoryName string) (*platformclientv2.Categoryresponselisting, *platformclientv2.APIResponse, error) {
	return p.knowledgeApi.GetKnowledgeKnowledgebaseCategories(knowledgeBaseId, pageSize, categoryName)
}

func getKnowledgeKnowledgebaseLabelsFn(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, pageSize int, labelName string) (*[]platformclientv2.Labellisting, *platformclientv2.APIResponse, error) {
	return p.knowledgeApi.GetKnowledgeKnowledgebaseLabels(knowledgeBaseId, pageSize, labelName)
}

func getKnowledgeKnowledgebaseLabelFn(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, labelId string) (*platformclientv2.Labelresponse, *platformclientv2.APIResponse, error) {
	return p.knowledgeApi.GetKnowledgeKnowledgebaseLabel(knowledgeBaseId, labelId)
}

func getKnowledgeKnowledgebaseDocumentFn(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, documentId string, expand []string, state string) (*platformclientv2.Knowledgedocumentresponse, *platformclientv2.APIResponse, error) {
	return p.knowledgeApi.GetKnowledgeKnowledgebaseDocument(knowledgeBaseId, documentId, expand, state)
}

func getAllKnowledgebaseEntitiesFn(ctx context.Context, p *knowledgeDocumentProxy, published bool) (*[]platformclientv2.Knowledgebase, *platformclientv2.APIResponse, error) {
	return p.knowledgeApi.GetKnowledgeKnowledgebases()
}

func createKnowledgeKnowledgebaseDocumentFn(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, body *platformclientv2.Knowledgedocumentreq) (*platformclientv2.Knowledgedocumentresponse, *platformclientv2.APIResponse, error) {
	return p.knowledgeApi.PostKnowledgeKnowledgebaseDocuments(knowledgeBaseId, *body)
}

func deleteKnowledgeKnowledgebaseDocumentFn(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, documentId string) (*platformclientv2.APIResponse, error) {
	return p.knowledgeApi.DeleteKnowledgeKnowledgebaseDocument(knowledgeBaseId, documentId)
}

func updateKnowledgeKnowledgebaseDocumentFn(ctx context.Context, p *knowledgeDocumentProxy, knowledgeBaseId string, documentId string, body *platformclientv2.Knowledgedocumentreq) (*platformclientv2.Knowledgedocumentresponse, *platformclientv2.APIResponse, error) {
	return p.knowledgeApi.PutKnowledgeKnowledgebaseDocument(knowledgeBaseId, documentId, *body)
}
