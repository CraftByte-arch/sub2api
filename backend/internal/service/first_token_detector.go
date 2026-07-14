package service

import "github.com/tidwall/gjson"

type FirstTokenProtocol string

const (
	ProtocolResponses         FirstTokenProtocol = "responses"
	ProtocolChatCompletions   FirstTokenProtocol = "chat_completions"
	ProtocolAnthropicMessages FirstTokenProtocol = "anthropic_messages"
)

// IsFirstSemanticToken reports whether data contains client-consumable stream
// progress. Lifecycle, usage, empty delta, and terminal events do not qualify.
func IsFirstSemanticToken(protocol FirstTokenProtocol, eventName string, data []byte) bool {
	if !gjson.ValidBytes(data) {
		return false
	}

	switch protocol {
	case ProtocolResponses:
		return isResponsesSemanticDelta(data)
	case ProtocolChatCompletions:
		return isChatCompletionsSemanticDelta(data)
	case ProtocolAnthropicMessages:
		return isAnthropicSemanticDelta(eventName, data)
	default:
		return false
	}
}

func isResponsesSemanticDelta(data []byte) bool {
	eventType, ok := stringField(gjson.GetBytes(data, "type"))
	if !ok {
		return false
	}

	switch eventType {
	case "response.output_text.delta",
		"response.reasoning_summary_text.delta",
		"response.reasoning_text.delta",
		"response.function_call_arguments.delta",
		"response.custom_tool_call_input.delta":
		return isNonEmptyString(gjson.GetBytes(data, "delta"))
	default:
		return false
	}
}

func isChatCompletionsSemanticDelta(data []byte) bool {
	choices := gjson.GetBytes(data, "choices")
	if !choices.IsArray() {
		return false
	}

	for _, choice := range choices.Array() {
		delta := choice.Get("delta")
		if !delta.IsObject() {
			continue
		}
		if isNonEmptyString(delta.Get("content")) ||
			isNonEmptyString(delta.Get("reasoning_content")) ||
			isNonEmptyString(delta.Get("reasoning")) ||
			isNonEmptyString(delta.Get("function_call.arguments")) {
			return true
		}

		toolCalls := delta.Get("tool_calls")
		if !toolCalls.IsArray() {
			continue
		}
		for _, toolCall := range toolCalls.Array() {
			if isNonEmptyString(toolCall.Get("function.arguments")) {
				return true
			}
		}
	}
	return false
}

func isAnthropicSemanticDelta(eventName string, data []byte) bool {
	payloadTypeValue := gjson.GetBytes(data, "type")
	if payloadTypeValue.Exists() && payloadTypeValue.Type != gjson.String {
		return false
	}
	payloadType, payloadTypePresent := stringField(payloadTypeValue)
	if eventName == "" {
		if !payloadTypePresent || payloadType != "content_block_delta" {
			return false
		}
	} else {
		if eventName != "content_block_delta" {
			return false
		}
		if payloadTypePresent && payloadType != "content_block_delta" {
			return false
		}
	}

	deltaType, ok := stringField(gjson.GetBytes(data, "delta.type"))
	if !ok {
		return false
	}
	switch deltaType {
	case "text_delta":
		return isNonEmptyString(gjson.GetBytes(data, "delta.text"))
	case "thinking_delta":
		return isNonEmptyString(gjson.GetBytes(data, "delta.thinking"))
	case "input_json_delta":
		return isNonEmptyString(gjson.GetBytes(data, "delta.partial_json"))
	default:
		return false
	}
}

func isNonEmptyString(value gjson.Result) bool {
	text, ok := stringField(value)
	return ok && len(text) > 0
}

func stringField(value gjson.Result) (string, bool) {
	if !value.Exists() || value.Type != gjson.String {
		return "", false
	}
	return value.String(), true
}
