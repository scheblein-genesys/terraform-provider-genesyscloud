package architect_flow

import (
	"context"
	"fmt"
	"log"
	rc "terraform-provider-genesyscloud/genesyscloud/resource_cache"
	"time"

	"github.com/mypurecloud/platform-client-sdk-go/v154/platformclientv2"
)

var internalProxy *architectFlowProxy

type getArchitectFunc func(context.Context, *architectFlowProxy, string) (*platformclientv2.Flow, *platformclientv2.APIResponse, error)
type forceUnlockFlowFunc func(context.Context, *architectFlowProxy, string) (*platformclientv2.APIResponse, error)
type deleteArchitectFlowFunc func(context.Context, *architectFlowProxy, string) (*platformclientv2.APIResponse, error)
type createArchitectFlowJobsFunc func(context.Context, *architectFlowProxy) (*platformclientv2.Registerarchitectjobresponse, *platformclientv2.APIResponse, error)
type getArchitectFlowJobsFunc func(context.Context, *architectFlowProxy, string) (*platformclientv2.Architectjobstateresponse, *platformclientv2.APIResponse, error)
type getAllArchitectFlowsFunc func(context.Context, *architectFlowProxy, string, []string) (*[]platformclientv2.Flow, *platformclientv2.APIResponse, error)
type getFlowIdByNameAndTypeFunc func(ctx context.Context, a *architectFlowProxy, name string, varType string) (id string, resp *platformclientv2.APIResponse, retryable bool, err error)

type generateDownloadUrlFunc func(a *architectFlowProxy, flowId string) (string, error)
type createExportJobFunc func(a *architectFlowProxy, flowId string) (jobId string, _ *platformclientv2.APIResponse, _ error)
type getExportJobStatusByIdFunc func(a *architectFlowProxy, jobId string) (*platformclientv2.Architectexportjobstateresponse, *platformclientv2.APIResponse, error)
type pollExportJobForDownloadUrlFunc func(a *architectFlowProxy, jobId string) (downloadUrl string, err error)

type architectFlowProxy struct {
	clientConfig *platformclientv2.Configuration
	api          *platformclientv2.ArchitectApi

	getArchitectFlowAttr            getArchitectFunc
	getAllArchitectFlowsAttr        getAllArchitectFlowsFunc
	forceUnlockFlowAttr             forceUnlockFlowFunc
	deleteArchitectFlowAttr         deleteArchitectFlowFunc
	createArchitectFlowJobsAttr     createArchitectFlowJobsFunc
	getArchitectFlowJobsAttr        getArchitectFlowJobsFunc
	getFlowIdByNameAndTypeAttr      getFlowIdByNameAndTypeFunc
	createExportJobAttr             createExportJobFunc
	getExportJobStatusByIdAttr      getExportJobStatusByIdFunc
	pollExportJobForDownloadUrlAttr pollExportJobForDownloadUrlFunc
	generateDownloadUrlAttr         generateDownloadUrlFunc

	flowCache rc.CacheInterface[platformclientv2.Flow]
}

var flowCache = rc.NewResourceCache[platformclientv2.Flow]()

func newArchitectFlowProxy(clientConfig *platformclientv2.Configuration) *architectFlowProxy {
	api := platformclientv2.NewArchitectApiWithConfig(clientConfig)
	return &architectFlowProxy{
		clientConfig: clientConfig,
		api:          api,

		getArchitectFlowAttr:            getArchitectFlowFn,
		getAllArchitectFlowsAttr:        getAllArchitectFlowsFn,
		forceUnlockFlowAttr:             forceUnlockFlowFn,
		deleteArchitectFlowAttr:         deleteArchitectFlowFn,
		createArchitectFlowJobsAttr:     createArchitectFlowJobsFn,
		getArchitectFlowJobsAttr:        getArchitectFlowJobsFn,
		getFlowIdByNameAndTypeAttr:      getFlowIdByNameAndTypeFn,
		generateDownloadUrlAttr:         generateDownloadUrlFn,
		createExportJobAttr:             createExportJobFn,
		getExportJobStatusByIdAttr:      getExportJobStatusByIdFn,
		pollExportJobForDownloadUrlAttr: pollExportJobForDownloadUrlFn,
		flowCache:                       flowCache,
	}
}

func getArchitectFlowProxy(clientConfig *platformclientv2.Configuration) *architectFlowProxy {
	if internalProxy == nil {
		internalProxy = newArchitectFlowProxy(clientConfig)
	}
	return internalProxy
}

func (a *architectFlowProxy) GetFlow(ctx context.Context, id string) (*platformclientv2.Flow, *platformclientv2.APIResponse, error) {
	return a.getArchitectFlowAttr(ctx, a, id)
}

func (a *architectFlowProxy) ForceUnlockFlow(ctx context.Context, id string) (*platformclientv2.APIResponse, error) {
	return a.forceUnlockFlowAttr(ctx, a, id)
}

func (a *architectFlowProxy) DeleteFlow(ctx context.Context, id string) (*platformclientv2.APIResponse, error) {
	return a.deleteArchitectFlowAttr(ctx, a, id)
}

func (a *architectFlowProxy) CreateFlowsDeployJob(ctx context.Context) (*platformclientv2.Registerarchitectjobresponse, *platformclientv2.APIResponse, error) {
	return a.createArchitectFlowJobsAttr(ctx, a)
}

func (a *architectFlowProxy) GetFlowsDeployJob(ctx context.Context, jobId string) (*platformclientv2.Architectjobstateresponse, *platformclientv2.APIResponse, error) {
	return a.getArchitectFlowJobsAttr(ctx, a, jobId)
}

func (a *architectFlowProxy) GetAllFlows(ctx context.Context, name string, varType []string) (*[]platformclientv2.Flow, *platformclientv2.APIResponse, error) {
	return a.getAllArchitectFlowsAttr(ctx, a, name, varType)
}

// getFlowIdByNameAndType retrieves a flow ID by searching for a flow with the specified name and type.
// It returns an error if multiple flows are found with the same name and none match the type, or if no matching flow is found.
//
// Implementation function: getFlowIdByNameAndTypeFn
// Parameters:
//   - ctx: The context.Context for the request
//   - a: A pointer to the architectFlowProxy instance
//   - name: The name of the flow to search for
//   - varType: The type of flow to filter by (optional, can be empty string)
//
// Returns:
//   - string: The ID of the matched flow
//   - *platformclientv2.APIResponse: The API response from the request
//   - bool: A boolean indicating if the error is retryable
//   - error: An error if the request fails, multiple flows are found, or no matching flow is found
func (a *architectFlowProxy) getFlowIdByNameAndType(ctx context.Context, name, varType string) (string, *platformclientv2.APIResponse, bool, error) {
	return a.getFlowIdByNameAndTypeAttr(ctx, a, name, varType)
}

// generateDownloadUrl generates a download URL for an architect flow export.
// Implementation function: generateDownloadUrlFn
//
// Parameters:
//   - a: *architectFlowProxy - The architect flow proxy instance
//   - flowId: string - The ID of the flow to be exported
//
// Returns:
//   - string: The download URL for the exported flow
//   - error: An error if the URL generation process fails
//
// The function performs the following steps:
// 1. Creates an export job for the specified flow
// 2. Polls the export job status until completion or timeout
// 3. Returns the download URL when the job completes successfully
//
// It will return an error if:
//   - The export job creation fails
//   - The job polling times out
//   - The job fails to generate a download URL
func (a *architectFlowProxy) generateDownloadUrl(flowId string) (string, error) {
	return a.generateDownloadUrlAttr(a, flowId)
}

// createExportJob creates an export job for a specified architect flow.
//
// Parameters:
//   - a: *architectFlowProxy - The architect flow proxy instance
//   - flowId: string - The ID of the flow to be exported
//
// Returns:
//   - string: The ID of the created export job
//   - *platformclientv2.APIResponse: The raw API response
//   - error: An error if the job creation fails or if the response is invalid
//
// The function creates an export job by making a POST request to the flows export jobs endpoint.
// It will return an error if:
//   - The API call fails
//   - The created job response is nil
//   - The job ID in the response is nil
func (a *architectFlowProxy) createExportJob(flowId string) (string, *platformclientv2.APIResponse, error) {
	return a.createExportJobAttr(a, flowId)
}

// getExportJobStatusById retrieves the status of an architect flow export job.
// Implementation function: getExportJobStatusByIdFn
// Parameters:
//   - jobId: string - The ID of the export job to check
//
// Returns:
//   - *platformclientv2.ArchitectFlowJobStatusResponse: The job status response
//   - *platformclientv2.APIResponse: The raw API response
//   - error: An error if the job status cannot be retrieved or if the response is invalid
//
// The function will return an error if the job status or status field is nil in the response.
func (a *architectFlowProxy) getExportJobStatusById(jobId string) (*platformclientv2.Architectexportjobstateresponse, *platformclientv2.APIResponse, error) {
	return a.getExportJobStatusByIdAttr(a, jobId)
}

// pollExportJobForDownloadUrl polls an architect flow export job until it completes or times out.
// It checks the job status periodically and waits for the download URL to become available.
// Implementation function: pollExportJobForDownloadUrlFn
//
// Parameters:
//   - a: *architectFlowProxy - The architect flow proxy instance
//   - jobId: string - The ID of the export job to poll
//
// Returns:
//   - string: The download URL for the exported flow
//   - error: An error if the polling times out or if there's an issue getting the job status
//
// The function will timeout after 90 seconds if the job doesn't complete.
// It polls the job status every 1 second.
func (a *architectFlowProxy) pollExportJobForDownloadUrl(jobId string) (downloadUrl string, err error) {
	return a.pollExportJobForDownloadUrlAttr(a, jobId)
}

func getFlowIdByNameAndTypeFn(ctx context.Context, a *architectFlowProxy, name, varType string) (string, *platformclientv2.APIResponse, bool, error) {
	var (
		matchedFlows []platformclientv2.Flow
		typeDetails  string
		types        []string
	)

	if varType != "" {
		types = append(types, varType)
		typeDetails = fmt.Sprintf("type '%s'", varType)
	}

	noFlowsFoundErr := fmt.Errorf("no flows found with name '%s' %s", name, typeDetails)

	flows, resp, err := a.GetAllFlows(ctx, name, types)
	if err != nil {
		return "", resp, false, err
	}

	if flows == nil || len(*flows) == 0 {
		return "", nil, true, noFlowsFoundErr
	}

	for _, flow := range *flows {
		if *flow.Name == name {
			matchedFlows = append(matchedFlows, flow)
		}
	}

	if len(matchedFlows) > 1 {
		for _, flow := range matchedFlows {
			if *flow.VarType == varType {
				return *flow.Id, nil, false, nil
			}
		}
		return "", nil, false, fmt.Errorf("found multiple flows with the name '%s', but none matched type '%s'", name, varType)
	}

	if len(matchedFlows) == 1 {
		return *matchedFlows[0].Id, nil, false, nil
	}

	return "", nil, true, noFlowsFoundErr
}

func getArchitectFlowFn(_ context.Context, p *architectFlowProxy, id string) (*platformclientv2.Flow, *platformclientv2.APIResponse, error) {
	flow := rc.GetCacheItem(p.flowCache, id)
	if flow != nil {
		return flow, nil, nil
	}
	return p.api.GetFlow(id, false)
}

func forceUnlockFlowFn(_ context.Context, p *architectFlowProxy, flowId string) (*platformclientv2.APIResponse, error) {
	log.Printf("Attempting to perform an unlock on flow: %s", flowId)
	_, resp, err := p.api.PostFlowsActionsUnlock(flowId)
	return resp, err
}

func deleteArchitectFlowFn(_ context.Context, p *architectFlowProxy, flowId string) (*platformclientv2.APIResponse, error) {
	return p.api.DeleteFlow(flowId)
}

func createArchitectFlowJobsFn(_ context.Context, p *architectFlowProxy) (*platformclientv2.Registerarchitectjobresponse, *platformclientv2.APIResponse, error) {
	return p.api.PostFlowsJobs()
}

func getArchitectFlowJobsFn(_ context.Context, p *architectFlowProxy, jobId string) (*platformclientv2.Architectjobstateresponse, *platformclientv2.APIResponse, error) {
	return p.api.GetFlowsJob(jobId, []string{"messages"})
}

func getAllArchitectFlowsFn(ctx context.Context, p *architectFlowProxy, name string, varType []string) (*[]platformclientv2.Flow, *platformclientv2.APIResponse, error) {
	const pageSize = 100
	var totalFlows []platformclientv2.Flow

	flows, resp, err := p.api.GetFlows(varType, 1, pageSize, "", "", nil, name, "", "", "", "", "", "", "", false, true, "", "", nil)
	if err != nil {
		return nil, resp, fmt.Errorf("failed to get page of flows: %v %v", err, resp)
	}
	if flows.Entities == nil || len(*flows.Entities) == 0 {
		return &totalFlows, nil, nil
	}

	totalFlows = append(totalFlows, *flows.Entities...)

	for pageNum := 2; pageNum <= *flows.PageCount; pageNum++ {
		flows, resp, err := p.api.GetFlows(varType, pageNum, pageSize, "", "", nil, name, "", "", "", "", "", "", "", false, true, "", "", nil)
		if err != nil {
			return nil, resp, fmt.Errorf("failed to get page %d of flows: %v", pageNum, err)
		}
		if flows.Entities == nil || len(*flows.Entities) == 0 {
			break
		}
		totalFlows = append(totalFlows, *flows.Entities...)
	}

	for _, flow := range totalFlows {
		rc.SetCache(p.flowCache, *flow.Id, flow)
	}

	return &totalFlows, nil, nil
}

func generateDownloadUrlFn(a *architectFlowProxy, flowId string) (string, error) {
	log.Printf("Creating export job for flow %s", flowId)
	jobId, resp, err := a.createExportJob(flowId)
	if err != nil {
		log.Println(err)
		if resp != nil {
			log.Printf("API Response: %s", resp.String())
		}
		return "", err
	}
	log.Printf("Successfully created export job '%s' for flow '%s'", jobId, flowId)

	log.Printf("Polling job '%s' for download url", jobId)
	downloadUrl, err := a.pollExportJobForDownloadUrl(jobId)
	if err != nil {
		return "", err
	}
	log.Printf("Successfully read download URL. Export job: %s", jobId)

	return downloadUrl, nil
}

func createExportJobFn(a *architectFlowProxy, flowId string) (string, *platformclientv2.APIResponse, error) {
	body := platformclientv2.Registerarchitectexportjob{
		Flows: &[]platformclientv2.Exportdetails{
			{
				Flow: &platformclientv2.Architectflowreference{
					Id: &flowId,
				},
			},
		},
	}

	createJob, resp, err := a.api.PostFlowsExportJobs(body)
	if err != nil {
		return "", resp, fmt.Errorf("failed to create export job for flow %s: %s", flowId, err.Error())
	}

	if createJob == nil || createJob.Id == nil {
		return "", resp, fmt.Errorf("no export job flow ID returned for flow %s", flowId)
	}

	return *createJob.Id, nil, nil
}

func getExportJobStatusByIdFn(a *architectFlowProxy, jobId string) (*platformclientv2.Architectexportjobstateresponse, *platformclientv2.APIResponse, error) {
	jobStatus, resp, err := a.api.GetFlowsExportJob(jobId, []string{"messages"})
	if err != nil {
		return nil, resp, fmt.Errorf("failed to get job status for job %s: %w", jobId, err)
	}

	if jobStatus == nil || jobStatus.Status == nil {
		return nil, resp, fmt.Errorf("export job %s response body did not provide a status", jobId)
	}

	return jobStatus, resp, nil
}

func pollExportJobForDownloadUrlFn(a *architectFlowProxy, jobId string) (string, error) {
	const maxWaitTimeInSeconds float64 = 90
	startTime := time.Now()

	for {
		elapsedTimeInSeconds := time.Since(startTime).Seconds()
		if elapsedTimeInSeconds > maxWaitTimeInSeconds {
			return "", fmt.Errorf("timed out after %f seconds waiting for job %s to complete", elapsedTimeInSeconds, jobId)
		}

		log.Printf("Sleeping for 1 seconds before polling job.")
		time.Sleep(1 * time.Second)

		exportJob, resp, err := a.getExportJobStatusById(jobId)
		if err != nil {
			log.Println(err)
			if resp != nil {
				log.Printf("API Response: %s", resp.String())
			}
			return "", err
		}

		status := *exportJob.Status

		if status == "Started" {
			continue
		} else if status == "Failure" {
			return "", fmt.Errorf("job %s failed. Messages: %s", jobId, parseMessagesFromExportJobStateResponse(*exportJob))
		} else if status != "Success" {
			return "", fmt.Errorf("unexpected job status %s for job %s", status, jobId)
		}

		if exportJob.DownloadUrl == nil {
			return "", fmt.Errorf("job %s was a success but no download ID was returned", jobId)
		}

		return *exportJob.DownloadUrl, nil
	}
}

// parseMessagesFromExportJobStateResponse extracts and formats messages from an architect export job state response.
//
// Parameters:
//   - job: platformclientv2.Architectexportjobstateresponse - The job state response object to parse messages from
//
// Returns:
//   - string: A formatted string containing all messages from the job response, with each message on a new line.
//     Returns an empty string if job.Messages is nil.
//
// The function concatenates all messages from the job response into a single string,
// with each message separated by a newline character. If the job response contains
// no messages (job.Messages is nil), an empty string is returned.
func parseMessagesFromExportJobStateResponse(job platformclientv2.Architectexportjobstateresponse) string {
	if job.Messages == nil {
		return ""
	}
	var messages string
	for _, m := range *job.Messages {
		messages += fmt.Sprintf("\n%s", m.String())
	}
	return messages
}
