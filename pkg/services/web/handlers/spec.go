package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-openapi/spec"
)

type SpecHandler struct{}

func NewSpecHandler() *SpecHandler {
	return &SpecHandler{}
}

func (h *SpecHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s := &spec.Swagger{
		SwaggerProps: spec.SwaggerProps{
			Swagger: "2.0", // go-openapi/spec defaults to 2.0, but we can try to use 3.0 fields if supported or stick to 2.0 which is widely compatible
			Info: &spec.Info{
				InfoProps: spec.InfoProps{
					Title:       "Constellation Overwatch API",
					Version:     "1.0.0",
					Description: "C4ISR (Command, Control, Communications, and Intelligence) server mesh for drone/robotic communication",
				},
			},
			BasePath: "/api/v1",
			Schemes:  []string{"http", "https"},
			Paths: &spec.Paths{
				Paths: map[string]spec.PathItem{
					"/health": {
						PathItemProps: spec.PathItemProps{
							Get: &spec.Operation{
								OperationProps: spec.OperationProps{
									Summary:     "Health Check",
									Description: "Get the health status of the API and its dependencies",
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												200: {
													ResponseProps: spec.ResponseProps{
														Description: "System is healthy",
													},
												},
											},
										},
									},
								},
							},
						},
					},
					"/sys/monitor/sse": {
						PathItemProps: spec.PathItemProps{
							Get: &spec.Operation{
								OperationProps: spec.OperationProps{
									Summary:     "System Monitor Stream",
									Description: "Server-Sent Events endpoint for system monitoring",
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												200: {
													ResponseProps: spec.ResponseProps{
														Description: "SSE Stream",
													},
												},
											},
										},
									},
								},
							},
						},
					},
					"/organizations": {
						PathItemProps: spec.PathItemProps{
							Get: &spec.Operation{
								OperationProps: spec.OperationProps{
									Summary: "List Organizations",
									Security: []map[string][]string{
										{"bearerAuth": {}},
									},
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												200: {
													ResponseProps: spec.ResponseProps{
														Description: "List of organizations",
														Schema: &spec.Schema{
															SchemaProps: spec.SchemaProps{
																Type: []string{"array"},
																Items: &spec.SchemaOrArray{
																	Schema: &spec.Schema{
																		SchemaProps: spec.SchemaProps{
																			Ref: spec.MustCreateRef("#/definitions/Organization"),
																		},
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
							Post: &spec.Operation{
								OperationProps: spec.OperationProps{
									Summary: "Create Organization",
									Security: []map[string][]string{
										{"bearerAuth": {}},
									},
									Parameters: []spec.Parameter{
										{
											ParamProps: spec.ParamProps{
												Name:     "body",
												In:       "body",
												Required: true,
												Schema: &spec.Schema{
													SchemaProps: spec.SchemaProps{
														Type:     []string{"object"},
														Required: []string{"name", "org_type"},
														Properties: map[string]spec.Schema{
															"name": {
																SchemaProps: spec.SchemaProps{
																	Type: []string{"string"},
																},
															},
															"org_type": {
																SchemaProps: spec.SchemaProps{
																	Type: []string{"string"},
																	Enum: []interface{}{"company", "agency", "individual"},
																},
															},
															"description": {
																SchemaProps: spec.SchemaProps{
																	Type: []string{"string"},
																},
															},
															"metadata": {
																SchemaProps: spec.SchemaProps{
																	Type: []string{"object"},
																},
															},
														},
													},
												},
											},
										},
									},
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												201: {
													ResponseProps: spec.ResponseProps{
														Description: "Organization created",
													},
												},
											},
										},
									},
								},
							},
							Delete: &spec.Operation{
								OperationProps: spec.OperationProps{
									Summary: "Delete Organization",
									Security: []map[string][]string{
										{"bearerAuth": {}},
									},
									Parameters: []spec.Parameter{
										{
											ParamProps: spec.ParamProps{
												Name:     "org_id",
												In:       "query",
												Required: true,
												Schema: &spec.Schema{
													SchemaProps: spec.SchemaProps{
														Type: []string{"string"},
													},
												},
											},
										},
									},
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												200: {
													ResponseProps: spec.ResponseProps{
														Description: "Organization deleted",
													},
												},
											},
										},
									},
								},
							},
						},
					},
					"/entities": {
						PathItemProps: spec.PathItemProps{
							Get: &spec.Operation{
								OperationProps: spec.OperationProps{
									Summary: "List Entities",
									Security: []map[string][]string{
										{"bearerAuth": {}},
									},
									Parameters: []spec.Parameter{
										{
											ParamProps: spec.ParamProps{
												Name:     "org_id",
												In:       "query",
												Required: true,
												Schema: &spec.Schema{
													SchemaProps: spec.SchemaProps{
														Type: []string{"string"},
													},
												},
											},
										},
									},
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												200: {
													ResponseProps: spec.ResponseProps{
														Description: "List of entities",
														Schema: &spec.Schema{
															SchemaProps: spec.SchemaProps{
																Type: []string{"array"},
																Items: &spec.SchemaOrArray{
																	Schema: &spec.Schema{
																		SchemaProps: spec.SchemaProps{
																			Ref: spec.MustCreateRef("#/definitions/Entity"),
																		},
																	},
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
							Post: &spec.Operation{
								OperationProps: spec.OperationProps{
									Summary: "Create Entity",
									Security: []map[string][]string{
										{"bearerAuth": {}},
									},
									Parameters: []spec.Parameter{
										{
											ParamProps: spec.ParamProps{
												Name:     "org_id",
												In:       "query",
												Required: true,
												Schema: &spec.Schema{
													SchemaProps: spec.SchemaProps{
														Type: []string{"string"},
													},
												},
											},
										},
										{
											ParamProps: spec.ParamProps{
												Name:     "body",
												In:       "body",
												Required: true,
												Schema: &spec.Schema{
													SchemaProps: spec.SchemaProps{
														Type:     []string{"object"},
														Required: []string{"name", "entity_type"},
														Properties: map[string]spec.Schema{
															"name": {
																SchemaProps: spec.SchemaProps{
																	Type: []string{"string"},
																},
															},
															"entity_type": {
																SchemaProps: spec.SchemaProps{
																	Type: []string{"string"},
																},
															},
															"status": {
																SchemaProps: spec.SchemaProps{
																	Type: []string{"string"},
																},
															},
															"priority": {
																SchemaProps: spec.SchemaProps{
																	Type: []string{"string"},
																},
															},
															"position": {
																SchemaProps: spec.SchemaProps{
																	Type: []string{"object"},
																	Properties: map[string]spec.Schema{
																		"latitude": {
																			SchemaProps: spec.SchemaProps{
																				Type: []string{"number"},
																			},
																		},
																		"longitude": {
																			SchemaProps: spec.SchemaProps{
																				Type: []string{"number"},
																			},
																		},
																		"altitude": {
																			SchemaProps: spec.SchemaProps{
																				Type: []string{"number"},
																			},
																		},
																	},
																},
															},
														},
													},
												},
											},
										},
									},
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												201: {
													ResponseProps: spec.ResponseProps{
														Description: "Entity created",
													},
												},
											},
										},
									},
								},
							},
							Put: &spec.Operation{
								OperationProps: spec.OperationProps{
									Summary: "Update Entity",
									Security: []map[string][]string{
										{"bearerAuth": {}},
									},
									Parameters: []spec.Parameter{
										{
											ParamProps: spec.ParamProps{
												Name:     "org_id",
												In:       "query",
												Required: true,
												Schema: &spec.Schema{
													SchemaProps: spec.SchemaProps{
														Type: []string{"string"},
													},
												},
											},
										},
										{
											ParamProps: spec.ParamProps{
												Name:     "entity_id",
												In:       "query",
												Required: true,
												Schema: &spec.Schema{
													SchemaProps: spec.SchemaProps{
														Type: []string{"string"},
													},
												},
											},
										},
										{
											ParamProps: spec.ParamProps{
												Name:     "body",
												In:       "body",
												Required: true,
												Schema: &spec.Schema{
													SchemaProps: spec.SchemaProps{
														Ref: spec.MustCreateRef("#/definitions/Entity"),
													},
												},
											},
										},
									},
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												200: {
													ResponseProps: spec.ResponseProps{
														Description: "Entity updated",
													},
												},
											},
										},
									},
								},
							},
							Delete: &spec.Operation{
								OperationProps: spec.OperationProps{
									Summary: "Delete Entity",
									Security: []map[string][]string{
										{"bearerAuth": {}},
									},
									Parameters: []spec.Parameter{
										{
											ParamProps: spec.ParamProps{
												Name:     "org_id",
												In:       "query",
												Required: true,
												Schema: &spec.Schema{
													SchemaProps: spec.SchemaProps{
														Type: []string{"string"},
													},
												},
											},
										},
										{
											ParamProps: spec.ParamProps{
												Name:     "entity_id",
												In:       "query",
												Required: true,
												Schema: &spec.Schema{
													SchemaProps: spec.SchemaProps{
														Type: []string{"string"},
													},
												},
											},
										},
									},
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												200: {
													ResponseProps: spec.ResponseProps{
														Description: "Entity deleted",
													},
												},
											},
										},
									},
								},
							},
						},
					},
					"/video/list": {
						PathItemProps: spec.PathItemProps{
							Get: &spec.Operation{
								OperationProps: spec.OperationProps{
									Summary:     "List Video Streams",
									Description: "SSE stream of available video sources",
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												200: {
													ResponseProps: spec.ResponseProps{
														Description: "Video stream list",
													},
												},
											},
										},
									},
								},
							},
						},
					},
					"/video/status": {
						PathItemProps: spec.PathItemProps{
							Get: &spec.Operation{
								OperationProps: spec.OperationProps{
									Summary:     "Video Status",
									Description: "Returns JSON status of all active MediaMTX video streams",
									Responses: &spec.Responses{
										ResponsesProps: spec.ResponsesProps{
											StatusCodeResponses: map[int]spec.Response{
												200: {
													ResponseProps: spec.ResponseProps{
														Description: "Stream status list",
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			Definitions: map[string]spec.Schema{
				"Organization": {
					SchemaProps: spec.SchemaProps{
						Type: []string{"object"},
						Properties: map[string]spec.Schema{
							"id": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"string"},
								},
							},
							"name": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"string"},
								},
							},
							"org_type": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"string"},
									Enum: []interface{}{"company", "agency", "individual"},
								},
							},
							"description": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"string"},
								},
							},
							"metadata": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"object"},
								},
							},
						},
					},
				},
				"Entity": {
					SchemaProps: spec.SchemaProps{
						Type: []string{"object"},
						Properties: map[string]spec.Schema{
							"id": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"string"},
								},
							},
							"org_id": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"string"},
								},
							},
							"entity_type": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"string"},
								},
							},
							"name": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"string"},
								},
							},
							"status": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"string"},
									Enum: []interface{}{"active", "inactive", "unknown"},
								},
							},
							"priority": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"string"},
									Enum: []interface{}{"low", "normal", "high", "critical"},
								},
							},
							"position": {
								SchemaProps: spec.SchemaProps{
									Type: []string{"object"},
									Properties: map[string]spec.Schema{
										"latitude": {
											SchemaProps: spec.SchemaProps{
												Type: []string{"number"},
											},
										},
										"longitude": {
											SchemaProps: spec.SchemaProps{
												Type: []string{"number"},
											},
										},
										"altitude": {
											SchemaProps: spec.SchemaProps{
												Type: []string{"number"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
			SecurityDefinitions: map[string]*spec.SecurityScheme{
				"bearerAuth": {
					SecuritySchemeProps: spec.SecuritySchemeProps{
						Type: "apiKey",
						Name: "Authorization",
						In:   "header",
					},
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}
