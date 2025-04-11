package provider

import (
	"context"
	"fmt"

	"github.com/go-errors/errors"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/client"
)

var (
	_ resource.Resource                = &paymentLinkResource{}
	_ resource.ResourceWithConfigure   = &paymentLinkResource{}
	_ resource.ResourceWithImportState = &paymentLinkResource{}
)

func NewPaymentLinkResource() resource.Resource {
	return &paymentLinkResource{}
}

type paymentLinkResource struct {
	client *client.API
}

// paymentLinkResourceModel maps the resource schema data.
type paymentLinkResourceModel struct {
	ID                  types.String `tfsdk:"id"`
	Active              types.Bool   `tfsdk:"active"`
	AllowPromotionCodes types.Bool   `tfsdk:"allow_promotion_codes"`
	Currency            types.String `tfsdk:"currency"`
	LineItems           types.List   `tfsdk:"line_items"`
	Metadata            types.Map    `tfsdk:"metadata"`
	SubmitType          types.String `tfsdk:"submit_type"`
	URL                 types.String `tfsdk:"url"`
	AfterCompletion     types.Object `tfsdk:"after_completion"`
}

// lineItemModel maps line item data.
type lineItemModel struct {
	Id                 types.String `tfsdk:"id"`
	Price              types.String `tfsdk:"price"`
	Quantity           types.Int64  `tfsdk:"quantity"`
	AdjustableQuantity types.Object `tfsdk:"adjustable_quantity"`
}

// adjustableQuantityModel maps adjustable quantity data.
type adjustableQuantityModel struct {
	Enabled types.Bool  `tfsdk:"enabled"`
	Minimum types.Int64 `tfsdk:"minimum"`
	Maximum types.Int64 `tfsdk:"maximum"`
}

// afterCompletionModel maps after completion data.
type afterCompletionModel struct {
	Type                            types.String `tfsdk:"type"`
	RedirectURL                     types.String `tfsdk:"redirect_url"`
	HostedConfirmationCustomMessage types.String `tfsdk:"hosted_confirmation_custom_message"`
}

func (r *paymentLinkResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_payment_link"
}

func (r *paymentLinkResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A payment link is a shareable URL that will take your customers to a hosted payment page.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "Unique identifier for the object.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"active": schema.BoolAttribute{
				Description: "Whether the payment link's `url` is active.",
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(true),
			},
			"allow_promotion_codes": schema.BoolAttribute{
				Description: "Enables user redeemable promotion codes.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"currency": schema.StringAttribute{
				Description: "Three-letter ISO currency code, in lowercase.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"line_items": schema.ListNestedAttribute{
				Description: "The line items representing what is being sold.",
				Required:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed:    true,
							Description: "Unique identifier for the object.",
							PlanModifiers: []planmodifier.String{
								stringplanmodifier.UseStateForUnknown(),
							},
						},
						"price": schema.StringAttribute{
							Required:    true,
							Description: "The ID of the Price or Plan object.",
						},
						"quantity": schema.Int64Attribute{
							Required:    true,
							Description: "The quantity of the line item being purchased.",
						},
						"adjustable_quantity": schema.SingleNestedAttribute{
							Optional:  true,
							WriteOnly: true,
							PlanModifiers: []planmodifier.Object{
								objectplanmodifier.UseStateForUnknown(),
							},
							Attributes: map[string]schema.Attribute{
								"enabled": schema.BoolAttribute{
									Required:  true,
									WriteOnly: true,
									PlanModifiers: []planmodifier.Bool{
										boolplanmodifier.UseStateForUnknown(),
									},
								},
								"maximum": schema.Int64Attribute{
									Optional:  true,
									WriteOnly: true,
									PlanModifiers: []planmodifier.Int64{
										int64planmodifier.UseStateForUnknown(),
									},
								},
								"minimum": schema.Int64Attribute{
									Optional:  true,
									WriteOnly: true,
									PlanModifiers: []planmodifier.Int64{
										int64planmodifier.UseStateForUnknown(),
									},
								},
							},
						},
					},
				},
			},
			"metadata": schema.MapAttribute{
				Description: "Set of key-value pairs that you can attach to an object.",
				ElementType: types.StringType,
				Optional:    true,
			},
			"submit_type": schema.StringAttribute{
				Description: "Describes the type of transaction being performed in order to customize relevant text on the page.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"url": schema.StringAttribute{
				Description: "The public URL that can be shared with customers.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"after_completion": schema.SingleNestedAttribute{
				Description: "Behavior after the purchase is complete.",
				Optional:    true,
				// Will receive a `hosted_confirmation` configuration if none specified
				Computed: true,
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Description: "The specified behavior after the purchase is complete.",
						Required:    true,
					},
					// Required if `type` is `redirect`
					"redirect_url": schema.StringAttribute{
						Description: "The URL the customer will be redirected to after the purchase is complete.",
						Optional:    true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseStateForUnknown(),
						},
					},
					// Required if `type` is `hosted_confirmation` but can be an empty string
					"hosted_confirmation_custom_message": schema.StringAttribute{
						Description: "A custom message to display to the customer after the purchase is complete.",
						Optional:    true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.UseStateForUnknown(),
						},
					},
				},
			},
		},
	}
}

func (r *paymentLinkResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*client.API)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *client.API, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client
}

// Certain params can't be set on specific endpoints. Easiest to simply disable these mappings.
type stripePaymentLinkParamOptions struct {
	canSetActive       bool
	canSetCurrency     bool
	canCreateLineItems bool
}

func mapPaymentLinkResourceStateToStripeParams(ctx context.Context, state *paymentLinkResourceModel, params *stripe.PaymentLinkParams, opts stripePaymentLinkParamOptions) diag.Diagnostics {
	var respDiags diag.Diagnostics

	if opts.canSetActive {
		if !state.Active.IsNull() && !state.Active.IsUnknown() {
			params.Active = stripe.Bool(state.Active.ValueBool())
		}
	}

	if !state.AllowPromotionCodes.IsNull() && !state.AllowPromotionCodes.IsUnknown() {
		params.AllowPromotionCodes = stripe.Bool(state.AllowPromotionCodes.ValueBool())
	}

	if opts.canSetCurrency {
		if !state.Currency.IsNull() && !state.Currency.IsUnknown() {
			params.Currency = stripe.String(state.Currency.ValueString())
		}
	}

	if !state.SubmitType.IsNull() && !state.SubmitType.IsUnknown() {
		params.SubmitType = stripe.String(state.SubmitType.ValueString())
	}

	// Handle line items
	var lineItems []lineItemModel
	diags := state.LineItems.ElementsAs(ctx, &lineItems, false)
	respDiags.Append(diags...)
	if respDiags.HasError() {
		err := errors.Errorf(`mapping error %s`, diags.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return respDiags
	}

	params.LineItems = make([]*stripe.PaymentLinkLineItemParams, len(lineItems))
	// TODO: Error if lineItems is empty
	for i, stateLineItem := range lineItems {
		stripeLineItem := &stripe.PaymentLinkLineItemParams{
			Quantity: stripe.Int64(stateLineItem.Quantity.ValueInt64()),
		}
		if opts.canCreateLineItems {
			stripeLineItem.Price = stripe.String(stateLineItem.Price.ValueString())
		} else {
			stripeLineItem.ID = stripe.String(stateLineItem.Id.ValueString())
		}

		if !stateLineItem.AdjustableQuantity.IsNull() {
			var adjQuantity adjustableQuantityModel
			diags = stateLineItem.AdjustableQuantity.As(ctx, &adjQuantity, basetypes.ObjectAsOptions{})
			respDiags.Append(diags...)
			if respDiags.HasError() {
				err := errors.Errorf(`mapping error %s`, diags.Errors()[0].Summary())
				tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
				return respDiags
			}

			stripeLineItem.AdjustableQuantity = &stripe.PaymentLinkLineItemAdjustableQuantityParams{
				Enabled: stripe.Bool(adjQuantity.Enabled.ValueBool()),
			}

			if !adjQuantity.Minimum.IsNull() {
				stripeLineItem.AdjustableQuantity.Minimum = stripe.Int64(adjQuantity.Minimum.ValueInt64())
			}

			if !adjQuantity.Maximum.IsNull() {
				stripeLineItem.AdjustableQuantity.Maximum = stripe.Int64(adjQuantity.Maximum.ValueInt64())
			}
		}

		params.LineItems[i] = stripeLineItem
	}

	// Handle after completion
	if !state.AfterCompletion.IsNull() && !state.AfterCompletion.IsUnknown() {
		var afterCompletion afterCompletionModel
		diags = state.AfterCompletion.As(ctx, &afterCompletion, basetypes.ObjectAsOptions{})
		respDiags.Append(diags...)
		if respDiags.HasError() {
			err := errors.Errorf(`mapping error %s`, diags.Errors()[0].Summary())
			tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
			return respDiags
		}

		params.AfterCompletion = &stripe.PaymentLinkAfterCompletionParams{
			Type: stripe.String(afterCompletion.Type.ValueString()),
		}

		if !afterCompletion.RedirectURL.IsNull() {
			params.AfterCompletion.Redirect = &stripe.PaymentLinkAfterCompletionRedirectParams{
				URL: stripe.String(afterCompletion.RedirectURL.ValueString()),
			}
		}

		if !afterCompletion.HostedConfirmationCustomMessage.IsNull() {
			params.AfterCompletion.HostedConfirmation = &stripe.PaymentLinkAfterCompletionHostedConfirmationParams{
				CustomMessage: stripe.String(afterCompletion.HostedConfirmationCustomMessage.ValueString()),
			}
		}
	}

	// Handle metadata
	if !state.Metadata.IsNull() {
		for k, v := range state.Metadata.Elements() {
			params.AddMetadata(k, v.(types.String).ValueString())
		}
	}

	return respDiags
}

func mapStripePaymentLinkToResourceState(ctx context.Context, stripeClient *client.API, paymentLink *stripe.PaymentLink, state *paymentLinkResourceModel, plan *paymentLinkResourceModel) diag.Diagnostics {
	var respDiags diag.Diagnostics

	state.ID = types.StringValue(paymentLink.ID)
	state.Active = types.BoolValue(paymentLink.Active)
	state.AllowPromotionCodes = types.BoolValue(paymentLink.AllowPromotionCodes)
	state.Currency = types.StringValue(string(paymentLink.Currency))
	state.URL = types.StringValue(paymentLink.URL)

	if paymentLink.SubmitType != "" {
		state.SubmitType = types.StringValue(string(paymentLink.SubmitType))
	} else {
		state.SubmitType = types.StringNull()
	}

	// Handle line items in state
	var planLineItems []lineItemModel
	diags := plan.LineItems.ElementsAs(ctx, &planLineItems, false)
	respDiags.Append(diags...)
	if respDiags.HasError() {
		err := errors.Errorf(`mapping error %s`, diags.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return respDiags
	}

	var stateLineItems []lineItemModel
	diags = state.LineItems.ElementsAs(ctx, &stateLineItems, false)
	respDiags.Append(diags...)
	if respDiags.HasError() {
		err := errors.Errorf(`mapping error %s`, diags.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return respDiags
	}

	// Handle after completion
	if paymentLink.AfterCompletion != nil {
		afterCompletion := afterCompletionModel{
			Type: types.StringValue(string(paymentLink.AfterCompletion.Type)),
		}

		if paymentLink.AfterCompletion.Redirect != nil {
			afterCompletion.RedirectURL = types.StringValue(paymentLink.AfterCompletion.Redirect.URL)
		} else {
			afterCompletion.RedirectURL = types.StringNull()
		}

		if paymentLink.AfterCompletion.HostedConfirmation != nil {
			afterCompletion.HostedConfirmationCustomMessage = types.StringValue(paymentLink.AfterCompletion.HostedConfirmation.CustomMessage)
		} else {
			afterCompletion.HostedConfirmationCustomMessage = types.StringNull()
		}

		afterCompletionAttrTypes := map[string]attr.Type{
			"type":                               types.StringType,
			"redirect_url":                       types.StringType,
			"hosted_confirmation_custom_message": types.StringType,
		}
		state.AfterCompletion, diags = types.ObjectValueFrom(ctx, afterCompletionAttrTypes, afterCompletion)
		respDiags.Append(diags...)
		if respDiags.HasError() {
			err := errors.Errorf(`mapping error %s`, diags.Errors()[0].Summary())
			tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
			return respDiags
		}
	}

	// Handle metadata
	if len(paymentLink.Metadata) == 0 && state.Metadata.IsNull() {
		// Preserve null state if the map is empty
		state.Metadata = types.MapNull(types.StringType)
	} else {
		metadata := make(map[string]attr.Value)
		for k, v := range paymentLink.Metadata {
			metadata[k] = types.StringValue(v)
		}
		state.Metadata, diags = types.MapValue(types.StringType, metadata)
		respDiags.Append(diags...)
		if respDiags.HasError() {
			err := errors.Errorf(`mapping error %s`, diags.Errors()[0].Summary())
			tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
			return respDiags
		}
	}

	// Fetch line items from the Stripe API
	listLineItemParams := &stripe.PaymentLinkListLineItemsParams{
		PaymentLink: stripe.String(paymentLink.ID),
	}
	lineItemsIter := stripeClient.PaymentLinks.ListLineItems(listLineItemParams)
	var lineItems []*stripe.LineItem
	for lineItemsIter.Next() {
		lineItems = append(lineItems, lineItemsIter.LineItem())
	}

	var updatedLineItems []attr.Value
	for i, item := range lineItems {
		lineItem := map[string]attr.Value{
			"id":       types.StringValue(item.ID),
			"price":    types.StringValue(item.Price.ID),
			"quantity": types.Int64Value(item.Quantity),
		}

		var stateLineItem *lineItemModel = nil
		if i < len(stateLineItems) {
			stateLineItem = &stateLineItems[i]
		}

		var planLineItem *lineItemModel = nil
		if i < len(planLineItems) {
			planLineItem = &planLineItems[i]
		}

		// We need to preserve the adjustable_quantity from the request
		// since it's not available in the API response
		if planLineItem != nil && !planLineItem.AdjustableQuantity.IsNull() {
			// adjustable_quantity was updated by current plan, so use that
			lineItem["adjustable_quantity"] = planLineItem.AdjustableQuantity
		} else if stateLineItem != nil && !stateLineItem.AdjustableQuantity.IsNull() {
			// preserve adjustable quantity that exists on previous state
			lineItem["adjustable_quantity"] = stateLineItem.AdjustableQuantity
		} else {
			// If no adjustable_quantity was specified in the request, set it to null
			lineItem["adjustable_quantity"] = types.ObjectNull(map[string]attr.Type{
				"enabled": types.BoolType,
				"minimum": types.Int64Type,
				"maximum": types.Int64Type,
			})
		}

		// Add the complete line item to the list of state line items
		updatedLineItem, diags := types.ObjectValue(
			map[string]attr.Type{
				"id":       types.StringType,
				"price":    types.StringType,
				"quantity": types.Int64Type,
				"adjustable_quantity": types.ObjectType{AttrTypes: map[string]attr.Type{
					"enabled": types.BoolType,
					"minimum": types.Int64Type,
					"maximum": types.Int64Type,
				}},
			},
			lineItem,
		)
		respDiags.Append(diags...)
		if respDiags.HasError() {
			err := errors.Errorf(`mapping error %s`, diags.Errors()[0].Summary())
			tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
			return respDiags
		}

		updatedLineItems = append(updatedLineItems, updatedLineItem)
	}

	state.LineItems, diags = types.ListValue(
		types.ObjectType{AttrTypes: map[string]attr.Type{
			"id":       types.StringType,
			"price":    types.StringType,
			"quantity": types.Int64Type,
			"adjustable_quantity": types.ObjectType{AttrTypes: map[string]attr.Type{
				"enabled": types.BoolType,
				"minimum": types.Int64Type,
				"maximum": types.Int64Type,
			}},
		}},
		updatedLineItems,
	)
	respDiags.Append(diags...)
	if respDiags.HasError() {
		err := errors.Errorf(`mapping error %s`, diags.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return respDiags
	}

	return respDiags
}

func (r *paymentLinkResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan paymentLinkResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		err := errors.Errorf(`mapping error %s`, resp.Diagnostics.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return
	}

	tflog.Debug(ctx, "Creating Stripe payment link")

	params := &stripe.PaymentLinkParams{}

	// Set basic parameters
	diags = mapPaymentLinkResourceStateToStripeParams(ctx, &plan, params, stripePaymentLinkParamOptions{canSetActive: false, canSetCurrency: true, canCreateLineItems: true})
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		err := errors.Errorf(`mapping error %s`, resp.Diagnostics.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return
	}
	if !plan.Active.IsNull() && !plan.Active.IsUnknown() {
		if !plan.Active.ValueBool() {
			// TODO: Support this case by immediately updating active to false after the link is created.
			resp.Diagnostics.AddError(
				"Error creating Stripe payment link",
				"A payment link cannot be created with `active = false`.",
			)
			return
		}

		// The create method does not support the `active` param, so unset this param. The link will be active by default.
		params.Active = nil
	}

	// Create the payment link
	paymentLink, err := r.client.PaymentLinks.New(params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error creating Stripe payment link",
			fmt.Sprintf("Could not create Stripe payment link: %s", err.Error()),
		)
		return
	}

	// Map response body to model
	state := plan
	diags = mapStripePaymentLinkToResourceState(ctx, r.client, paymentLink, &state, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		err := errors.Errorf(`mapping error %s`, resp.Diagnostics.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return
	}

	// Set state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		err := errors.Errorf(`mapping error %s`, resp.Diagnostics.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return
	}

	tflog.Debug(ctx, "Created Stripe payment link", map[string]any{"id": state.ID.ValueString()})
}

func (r *paymentLinkResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state paymentLinkResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		err := errors.Errorf(`mapping error %s`, resp.Diagnostics.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return
	}

	tflog.Debug(ctx, "Reading Stripe payment link", map[string]any{"id": state.ID.ValueString()})

	paymentLink, err := r.client.PaymentLinks.Get(state.ID.ValueString(), nil)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error reading Stripe payment link",
			fmt.Sprintf("Could not read Stripe payment link %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	// Map response body to model
	mapStripePaymentLinkToResourceState(ctx, r.client, paymentLink, &state, &state)

	// Set state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		err := errors.Errorf(`mapping error %s`, resp.Diagnostics.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return
	}

	tflog.Debug(ctx, "Read Stripe payment link", map[string]any{"id": state.ID.ValueString()})
}

func (r *paymentLinkResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan paymentLinkResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		err := errors.Errorf(`mapping error %s`, resp.Diagnostics.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return
	}

	var state paymentLinkResourceModel
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		err := errors.Errorf(`mapping error %s`, resp.Diagnostics.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return
	}

	tflog.Debug(ctx, "Updating Stripe payment link", map[string]any{"id": state.ID.ValueString()})

	params := &stripe.PaymentLinkParams{}
	diags = mapPaymentLinkResourceStateToStripeParams(ctx, &plan, params, stripePaymentLinkParamOptions{canSetActive: true, canSetCurrency: false, canCreateLineItems: false})
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		err := errors.Errorf(`mapping error %s`, resp.Diagnostics.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return
	}

	// Update the payment link
	paymentLink, err := r.client.PaymentLinks.Update(state.ID.ValueString(), params)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error updating Stripe payment link",
			fmt.Sprintf("Could not update Stripe payment link %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	// Map response body to model
	mapStripePaymentLinkToResourceState(ctx, r.client, paymentLink, &state, &plan)

	// Set state
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		err := errors.Errorf(`mapping error %s`, resp.Diagnostics.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return
	}

	tflog.Debug(ctx, "Updated Stripe payment link", map[string]any{"id": state.ID.ValueString()})
}

func (r *paymentLinkResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state paymentLinkResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		err := errors.Errorf(`mapping error %s`, resp.Diagnostics.Errors()[0].Summary())
		tflog.Error(ctx, fmt.Sprintf(`returning error: %s`, err.ErrorStack()))
		return
	}

	tflog.Debug(ctx, "Deactivating Stripe payment link", map[string]any{"id": state.ID.ValueString()})

	// Deactivate the payment link
	_, err := r.client.PaymentLinks.Update(state.ID.ValueString(), &stripe.PaymentLinkParams{
		Active: stripe.Bool(false),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error deactivating Stripe payment link",
			fmt.Sprintf("Could not deactivate Stripe payment link %s: %s", state.ID.ValueString(), err.Error()),
		)
		return
	}

	tflog.Debug(ctx, "Deactivated Stripe payment link", map[string]any{"id": state.ID.ValueString()})
}

func (r *paymentLinkResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
