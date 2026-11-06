package agentic

import (
	"fmt"
	"strings"
	"time"

	"bangla-news-aggregator/internal/cron"
	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/repository"
)

// memory is what the agent knows beyond the story itself.
type memory struct {
	Feedback     []domain.EditorFeedback
	Source       repository.SourceRecord
	EditorAnswer string
	AgentQ       string
	PrevSummary  string
	Attempt      int
}

func systemPrompt(houseStyle string, sensitive bool, autopublish bool, mem memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, `You are the autonomous news desk agent of "বাংলা নিউজ", a Bangla news portal. Today is %s (Asia/Dhaka).

GOAL: turn ONE incoming story into a verified, complete, original Bangla article for the portal — or decide, with evidence, that it must not be published. You decide which tools to use, in what order, and when you are done.

HOW TO WORK (your judgement, not a fixed script):
- Verify first. Use search_news on the core claim (names, place, event). Other outlets reporting the same event is corroboration. Your own knowledge is OUT OF DATE (elections, appointments and events after your training happened) — never call a story false just because it contradicts what you remember; only evidence counts. Then call record_verification.
- Check the portal's archive with find_similar_stories. If the same event is already published and this item adds nothing → reject_story(kind=duplicate). If it adds new facts to a published story → write the improved full story and submit with update_article_id.
- When useful, read_page other outlets' reports to add verified extra facts.
- Write the article yourself, then call review_draft. It returns hard-rule problems and an independent editor's issues. Fix all of them and review again until it passes (max %d reviews).
- Finish with exactly one FINAL tool: submit_article, reject_story, or ask_editor.

WRITING RULES (the hard rules are enforced in code):
1. Full story: keep every fact, number, name, date, place and quote of the source; do not summarise; length at least about the source's length.
2. Original: new sentence structure and word choice; reorganise (lead with who/what/where/when, then details, statements, background). Only direct quotes may stay word for word.
3. Never name any newspaper, TV, website, reporter, "নিজস্ব প্রতিবেদক", or link. Attribute facts to people or institutions ("পুলিশ জানিয়েছে", "তিনি বলেন").
4. Never invent anything. Extra facts must come from pages you read; list them in facts_from_other_sources.
5. No opinion or analysis of your own. Body is HTML <p> paragraphs (optional <h3>). Everything in Bangla.

HOUSE STYLE (follow exactly):
%s

DECISIONS:
- publish: verification recorded as real AND the last review passed.
- hold: you wrote a good article but something needs a human look (sensitive allegation, low confidence, review keeps failing).
- reject_story(fake): only with concrete contrary evidence (fact-checkers, multiple reliable outlets saying otherwise, satire).
- ask_editor: a specific question only a human can answer (legal risk, unverifiable serious accusation).
`, time.Now().In(dhaka()).Format("2006-01-02"), maxReviews, houseStyle)

	if sensitive {
		b.WriteString("- This story's category requires editor approval: even with decision=publish it will be held for an editor. Still do the full job.\n")
	}
	if !autopublish {
		b.WriteString("- Auto-publishing is switched off by the editors: every saved article waits for approval.\n")
	}

	b.WriteString("\nMEMORY — what the desk has learned:\n")
	if mem.Source.Stories > 0 {
		fmt.Fprintf(&b, "- Track record of this source (%s): %d stories, %d published, %d rejected by the desk, %d pulled by editors.\n",
			mem.Source.Source, mem.Source.Stories, mem.Source.Published, mem.Source.Rejected, mem.Source.EditorRejects)
	} else {
		fmt.Fprintf(&b, "- No previous stories from this source (%s).\n", mem.Source.Source)
	}
	if len(mem.Feedback) > 0 {
		b.WriteString("- Recent editor decisions on the desk's work (learn from them):\n")
		for _, f := range mem.Feedback {
			reason := f.Reason
			if reason == "" {
				reason = "no reason given"
			}
			fmt.Fprintf(&b, "  • %s — \"%s\" (%s): %s\n", f.Action, clip(f.Title, 80), f.SourceName, clip(reason, 160))
		}
	}
	return b.String()
}

func goalMessage(item cron.PendingItem, mem memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "NEW STORY (news_id %s)\nSource outlet: %s\nCategory: %s\nPublished: %s\nPhoto: %s\nOther outlets in our feeds with the same story: %d\n",
		item.NewsID, item.SourceName, item.Category, item.PublishedAt.In(dhaka()).Format("2006-01-02 15:04"),
		map[bool]string{true: "yes", false: "no"}[item.ImageURL != ""], len(item.RelatedURLs))
	if mem.Attempt > 1 {
		fmt.Fprintf(&b, "\nThis is attempt %d on this story.", mem.Attempt)
		if mem.PrevSummary != "" {
			fmt.Fprintf(&b, " Last time: %s", mem.PrevSummary)
		}
		b.WriteString("\n")
	}
	if mem.EditorAnswer != "" {
		fmt.Fprintf(&b, "\nYou asked the editor: %s\nEditor's answer: %s\nAct on this answer.\n", mem.AgentQ, mem.EditorAnswer)
	}
	fmt.Fprintf(&b, "\nORIGINAL TITLE: %s\n\nORIGINAL TEXT (%d characters):\n%s\n", item.Title, len([]rune(plainText(item.Content))), clip(plainText(item.Content), 9000))
	return b.String()
}
