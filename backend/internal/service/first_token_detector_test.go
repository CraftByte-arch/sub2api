package service

import "testing"

func TestFirstTokenDetector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		protocol  FirstTokenProtocol
		eventName string
		data      string
		want      bool
	}{
		{
			name:     "responses output text",
			protocol: ProtocolResponses,
			data:     `{"type":"response.output_text.delta","delta":"hello"}`,
			want:     true,
		},
		{
			name:     "responses reasoning summary",
			protocol: ProtocolResponses,
			data:     `{"type":"response.reasoning_summary_text.delta","delta":"thinking"}`,
			want:     true,
		},
		{
			name:     "responses raw reasoning",
			protocol: ProtocolResponses,
			data:     `{"type":"response.reasoning_text.delta","delta":"thinking"}`,
			want:     true,
		},
		{
			name:     "responses function arguments",
			protocol: ProtocolResponses,
			data:     `{"type":"response.function_call_arguments.delta","delta":"{\"city\":"}`,
			want:     true,
		},
		{
			name:     "responses custom tool input",
			protocol: ProtocolResponses,
			data:     `{"type":"response.custom_tool_call_input.delta","delta":"run tests"}`,
			want:     true,
		},
		{
			name:     "responses whitespace is semantic",
			protocol: ProtocolResponses,
			data:     `{"type":"response.output_text.delta","delta":" "}`,
			want:     true,
		},
		{
			name:     "responses lifecycle metadata",
			protocol: ProtocolResponses,
			data:     `{"type":"response.created","response":{"id":"resp_1"}}`,
		},
		{
			name:     "responses output item added",
			protocol: ProtocolResponses,
			data:     `{"type":"response.output_item.added","item":{"type":"message"}}`,
		},
		{
			name:     "responses usage only",
			protocol: ProtocolResponses,
			data:     `{"type":"response.completed","response":{"usage":{"output_tokens":1}}}`,
		},
		{
			name:     "responses empty delta",
			protocol: ProtocolResponses,
			data:     `{"type":"response.output_text.delta","delta":""}`,
		},
		{
			name:     "responses numeric delta",
			protocol: ProtocolResponses,
			data:     `{"type":"response.output_text.delta","delta":1}`,
		},
		{
			name:     "responses object delta",
			protocol: ProtocolResponses,
			data:     `{"type":"response.output_text.delta","delta":{"text":"hello"}}`,
		},
		{
			name:     "responses null delta",
			protocol: ProtocolResponses,
			data:     `{"type":"response.output_text.delta","delta":null}`,
		},
		{
			name:     "responses terminal done text is not delta",
			protocol: ProtocolResponses,
			data:     `{"type":"response.output_text.done","text":"hello"}`,
		},
		{
			name:     "responses failed terminal",
			protocol: ProtocolResponses,
			data:     `{"type":"response.failed","response":{"status":"failed"}}`,
		},
		{
			name:     "chat content",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"index":0,"delta":{"content":"hello"}}]}`,
			want:     true,
		},
		{
			name:     "chat reasoning content",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"index":0,"delta":{"reasoning_content":"thinking"}}]}`,
			want:     true,
		},
		{
			name:     "chat compatible reasoning",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"index":0,"delta":{"reasoning":"thinking"}}]}`,
			want:     true,
		},
		{
			name:     "chat legacy function arguments",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"index":0,"delta":{"function_call":{"name":"weather","arguments":"{\"city\":"}}}]}`,
			want:     true,
		},
		{
			name:     "chat tool arguments",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"weather","arguments":"{\"city\":"}}]}}]}`,
			want:     true,
		},
		{
			name:     "chat whitespace is semantic",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"index":0,"delta":{"content":" "}}]}`,
			want:     true,
		},
		{
			name:     "chat later choice has content",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"index":0,"delta":{"content":""}},{"index":1,"delta":{"content":"later"}}]}`,
			want:     true,
		},
		{
			name:     "chat later tool has arguments",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"first","arguments":""}},{"index":1,"function":{"name":"second","arguments":"{}"}}]}}]}`,
			want:     true,
		},
		{
			name:     "chat role only",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
		},
		{
			name:     "chat finish reason",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		},
		{
			name:     "chat usage only",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2}}`,
		},
		{
			name:     "chat empty choices",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[]}`,
		},
		{
			name:     "chat empty delta fields",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"delta":{"content":"","reasoning_content":"","function_call":{"arguments":""},"tool_calls":[{"function":{"arguments":""}}]}}]}`,
		},
		{
			name:     "chat choices is not array",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":{"delta":{"content":"hello"}}}`,
		},
		{
			name:     "chat choice delta is not object",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"delta":"hello"}]}`,
		},
		{
			name:     "chat content is not string",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"delta":{"content":1}}]}`,
		},
		{
			name:     "chat tool calls is not array",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"delta":{"tool_calls":{"function":{"arguments":"{}"}}}}]}`,
		},
		{
			name:     "chat function arguments is not string",
			protocol: ProtocolChatCompletions,
			data:     `{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":1}}]}}]}`,
		},
		{
			name:      "anthropic text",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
			want:      true,
		},
		{
			name:      "anthropic thinking",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"thinking"}}`,
			want:      true,
		},
		{
			name:      "anthropic tool input",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"city\":"}}`,
			want:      true,
		},
		{
			name:      "anthropic whitespace is semantic",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" "}}`,
			want:      true,
		},
		{
			name:     "anthropic payload type works without event name",
			protocol: ProtocolAnthropicMessages,
			data:     `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
			want:     true,
		},
		{
			name:      "anthropic event name works without payload type",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"index":0,"delta":{"type":"text_delta","text":"hello"}}`,
			want:      true,
		},
		{
			name:      "anthropic message start",
			protocol:  ProtocolAnthropicMessages,
			eventName: "message_start",
			data:      `{"type":"message_start","message":{"role":"assistant"}}`,
		},
		{
			name:      "anthropic message delta usage",
			protocol:  ProtocolAnthropicMessages,
			eventName: "message_delta",
			data:      `{"type":"message_delta","usage":{"output_tokens":1}}`,
		},
		{
			name:      "anthropic ping",
			protocol:  ProtocolAnthropicMessages,
			eventName: "ping",
			data:      `{"type":"ping"}`,
		},
		{
			name:      "anthropic content block start",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_start",
			data:      `{"type":"content_block_start","content_block":{"type":"text","text":"hello"}}`,
		},
		{
			name:      "anthropic empty delta",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"content_block_delta","delta":{"type":"text_delta","text":""}}`,
		},
		{
			name:      "anthropic signature delta is not semantic",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"content_block_delta","delta":{"type":"signature_delta","signature":"signature"}}`,
		},
		{
			name:      "anthropic message stop",
			protocol:  ProtocolAnthropicMessages,
			eventName: "message_stop",
			data:      `{"type":"message_stop"}`,
		},
		{
			name:      "anthropic content block stop",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_stop",
			data:      `{"type":"content_block_stop","index":0}`,
		},
		{
			name:      "anthropic lifecycle event overrides delta-like payload",
			protocol:  ProtocolAnthropicMessages,
			eventName: "message_start",
			data:      `{"delta":{"type":"text_delta","text":"hello"}}`,
		},
		{
			name:      "anthropic delta event conflicts with lifecycle payload",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"message_start","delta":{"type":"text_delta","text":"hello"}}`,
		},
		{
			name:      "anthropic lifecycle event conflicts with delta payload",
			protocol:  ProtocolAnthropicMessages,
			eventName: "message_start",
			data:      `{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`,
		},
		{
			name:      "anthropic non-string payload type",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":1,"delta":{"type":"text_delta","text":"hello"}}`,
		},
		{
			name:      "anthropic delta is not object",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"content_block_delta","delta":"hello"}`,
		},
		{
			name:      "anthropic delta type is not string",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"content_block_delta","delta":{"type":1,"text":"hello"}}`,
		},
		{
			name:      "anthropic text is not string",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"content_block_delta","delta":{"type":"text_delta","text":1}}`,
		},
		{
			name:      "anthropic thinking is not string",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":{"text":"hello"}}}`,
		},
		{
			name:      "anthropic partial json is not string",
			protocol:  ProtocolAnthropicMessages,
			eventName: "content_block_delta",
			data:      `{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":null}}`,
		},
		{
			name:     "anthropic lifecycle payload without event name",
			protocol: ProtocolAnthropicMessages,
			data:     `{"type":"message_start","delta":{"type":"text_delta","text":"hello"}}`,
		},
		{
			name:     "malformed JSON",
			protocol: ProtocolResponses,
			data:     `{"type":"response.output_text.delta","delta":"hello"`,
		},
		{
			name:     "unknown protocol",
			protocol: FirstTokenProtocol("unknown"),
			data:     `{"type":"response.output_text.delta","delta":"hello"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsFirstSemanticToken(tt.protocol, tt.eventName, []byte(tt.data)); got != tt.want {
				t.Fatalf("IsFirstSemanticToken(%q, %q, %s) = %v, want %v", tt.protocol, tt.eventName, tt.data, got, tt.want)
			}
		})
	}
}
