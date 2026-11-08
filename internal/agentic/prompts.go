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
	Facts        string
}

func systemPrompt(houseStyle string, sensitive bool, autopublish bool, mem memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, `You are the autonomous news desk agent of "বাংলা নিউজ", a Bangla news portal. Today is %s (Asia/Dhaka).

%s
GOAL: turn ONE incoming story into a verified, complete, original Bangla article for the portal — or decide, with evidence, that it must not be published. You decide which tools to use, in what order, and when you are done.

HOW TO WORK (your judgement, not a fixed script):
- Be efficient: every turn costs quota, and a new story should reach readers every 15 minutes. The desk has already run search_news on the headline and checked the archive (see ALREADY GATHERED). When that settles it, do it in as few turns as possible:
  turn 1: record_verification AND write_draft, both in the same turn;
  turn 2: submit_article if write_draft's review passed — otherwise fix it (write_draft again with notes, or review_draft with corrections).
- Verify before deciding. Other outlets reporting the same event is corroboration (the search tells you how many matched). Search more (other key words, language=en) only if the gathered results are not enough. Then call record_verification.
- YOUR MEMORY IS OUT OF DATE. Elections, new governments, appointments and resignations have happened since your training. If the story and other outlets say someone holds a position (Prime Minister, minister, party leader…), that IS the current reality: keep it as the source says. Never "correct" a source to match your memory, never call a story false or ask an editor because of such a conflict. If you learn such a fact from 2+ outlets, call remember_fact so future stories know it.
- Archive (find_similar_stories): if the same event is already published and this item adds nothing → reject_story(kind=duplicate). If it adds new facts to a published story → write the improved full story and submit with update_article_id.
- Only when needed, read_page another outlet's report to add verified extra facts.
- Produce the article with write_draft: the desk writer rewrites the WHOLE source passage by passage (full length, no copying, new headline) and the draft is reviewed at once — hard-rule problems plus an independent editor's issues. Fix what it reports and review again until it passes (max %d reviews in total).
- Finish with exactly one FINAL tool: submit_article, reject_story, or ask_editor.

WRITING RULES (the hard rules are enforced in code):
1. Full story: keep every fact, number, name, date, place and quote of the source; do not summarise; length at least about the source's length.
2. Original: new sentence structure and word choice; reorganise (lead with who/what/where/when, then details, statements, background). A speaker's exact words may stay word for word ONLY as a direct quote in quotation marks ("…"), attributed to the speaker — use this for long speeches you cannot reword.
3. Never name any newspaper, TV, website, reporter, "নিজস্ব প্রতিবেদক", or link. Attribute facts to people or institutions ("পুলিশ জানিয়েছে", "তিনি বলেন").
4. Never invent anything. Extra facts must come from pages you read; list them in facts_from_other_sources.
5. No opinion or analysis of your own. Body is HTML <p> paragraphs (optional <h3>). Everything in Bangla.

HOUSE STYLE (follow exactly):
%s

DECISIONS:
- publish: verification recorded as real AND the last review passed.
- hold: you wrote a good article but something needs a human look (sensitive allegation, low confidence, review keeps failing).
- reject_story(fake): only with concrete contrary evidence (fact-checkers, multiple reliable outlets saying otherwise, satire).
- ask_editor: ONLY for legal risk, a serious accusation you cannot verify, or clearly sensitive content. Never ask about passing the review (fix it, or hold) or about facts that differ from your memory (the sources and CURRENT FACTS are right).
`, time.Now().In(dhaka()).Format("2006-01-02"), factsBlock(mem.Facts), maxReviews, houseStyle)

	if sensitive {
		fmt.Fprintf(&b, "- This story's category is sensitive: decision=publish goes live on its own only if at least %d other outlets were confirmed carrying the same story (counted by search_news) and the review passed; otherwise it waits for an editor. Search well so corroboration is counted.\n", cron.MinCorroboration)
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
	full := plainText(item.Content)
	fmt.Fprintf(&b, "\nORIGINAL TITLE: %s\n\nORIGINAL TEXT (%d characters; the beginning is shown — call read_source for all of it, write_draft and review_draft use the full text automatically):\n%s\n",
		item.Title, len([]rune(full)), clip(full, 3500))
	return b.String()
}

// factsBlock puts the editors' current facts at the very top of the prompt.
// The outlets a fact was confirmed by are left out: a writer shown them
// tends to name them in the story, which the portal never does.
func factsBlock(facts string) string {
	var lines []string
	for _, l := range strings.Split(facts, "\n") {
		if i := strings.Index(l, " — সূত্র:"); i >= 0 {
			l = l[:i]
		}
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, "- "+l)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "\nCURRENT FACTS — verified by the editors and by several outlets. They are true NOW and override anything you remember (they are background: never add them to a story that does not itself mention them):\n" + strings.Join(lines, "\n") + "\n"
}
