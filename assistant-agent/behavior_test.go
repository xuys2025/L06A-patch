package main

import "testing"

func TestExitPhraseNormalization(t *testing.T) {
	phrases := defaultConfig().ExitPhrases
	for _, input := range []string{"退下。", "退下吧", "小爱同学，退下吧！", "好了，请你退下吧", "可以退下了"} {
		if !isExitPhrase(input, phrases) {
			t.Errorf("expected %q to end the conversation", input)
		}
	}
	if isExitPhrase("请你介绍一下退下这个词", phrases) {
		t.Fatal("ordinary question was treated as an exit phrase")
	}
}

func TestEndConversationToolDetection(t *testing.T) {
	call := llmToolCall{}
	call.Function.Name = "end_conversation"
	if !hasEndConversationTool([]llmToolCall{call}) {
		t.Fatal("end_conversation tool call was not detected")
	}
	call.Function.Name = "other_tool"
	if hasEndConversationTool([]llmToolCall{call}) {
		t.Fatal("unrelated tool was treated as end_conversation")
	}
}
