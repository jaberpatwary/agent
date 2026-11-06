package ai

import "fmt"

// bangaEditorSystemPrompt is the shared system role used for the plain
// Summarize() call (kept for backward compatibility / fallback use). The
// richer agentic rewrite flow in internal/agent uses its own, configurable
// system prompt instead (see BuildRewriteDraftPrompt / BuildRewriteReviewPrompt).
const bangaEditorSystemPrompt = "You are a professional Bangla news editor who writes concise, accurate summaries in Bangla."

// buildSummaryPrompt builds a consistent Bangla-language summarization
// instruction shared by all providers, so Groq and Gemini output in a
// comparable style.
func buildSummaryPrompt(title, content string) string {
	return fmt.Sprintf(
		"শিরোনাম: %s\n\nমূল সংবাদ:\n%s\n\nউপরের সংবাদটি বাংলায় ৩-৪টি বাক্যে সংক্ষিপ্ত ও নিরপেক্ষভাবে সারসংক্ষেপ করো। শুধু সারসংক্ষেপ লেখো, অতিরিক্ত কোনো ভূমিকা বা মন্তব্য যোগ কোরো না।",
		title, content,
	)
}
