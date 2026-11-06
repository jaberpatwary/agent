package cron

import (
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"bangla-news-aggregator/internal/domain"
	"bangla-news-aggregator/internal/rss"
)

// candidate is one not-yet-saved RSS item paired with the feed it came from.
type candidate struct {
	item rss.Item
	feed domain.FeedSource
}

// storyCluster groups one or more candidates believed to cover the same
// real-world event. A cluster of size 1 is rewritten normally (see
// Worker.processSingle); a cluster of size 2+ is combined into one
// multi-source article (see Worker.processMulti).
type storyCluster struct {
	items []candidate
}

const (
	// Two headlines are only compared as the "same story" if their items
	// were published within this window of each other — otherwise an old
	// recurring phrase (e.g. a weekly column title) could wrongly cluster
	// unrelated days' stories together.
	clusterTimeWindow = 20 * time.Hour
	// Jaccard similarity of normalized title tokens required to cluster two
	// items together. Picked conservatively (prefers two near-duplicate
	// headlines to stay separate over wrongly merging two different
	// stories) since a bad merge is more visible to readers than a missed one.
	clusterSimThreshold = 0.45
	// Keeps synthesis prompts a bounded size regardless of how many feeds
	// happen to cover a very big story.
	clusterMaxSize = 4
)

// clusterCandidates groups candidates whose titles are similar enough, and
// whose publish times are close enough, to plausibly be independent
// coverage of the same event — while never grouping two items from the same
// feed (a feed republishing or updating its own story isn't "multi-source").
// This is a deliberately simple, local, embedding-free heuristic: cheap
// enough to run on every item of every cron pass, with no extra API calls.
func clusterCandidates(cands []candidate) []storyCluster {
	n := len(cands)
	tokenSets := make([]map[string]bool, n)
	for i, c := range cands {
		tokenSets[i] = titleTokens(c.item.Title)
	}

	assigned := make([]bool, n)
	clusters := make([]storyCluster, 0, n)

	for i := 0; i < n; i++ {
		if assigned[i] {
			continue
		}
		cl := storyCluster{items: []candidate{cands[i]}}
		assigned[i] = true

		for j := i + 1; j < n && len(cl.items) < clusterMaxSize; j++ {
			if assigned[j] {
				continue
			}
			if sameFeedAsAny(cl.items, cands[j].feed.ID) {
				continue
			}
			if cands[i].item.Published.IsZero() || cands[j].item.Published.IsZero() {
				continue // no reliable timestamp to judge "same news cycle"
			}
			diff := cands[j].item.Published.Sub(cands[i].item.Published)
			if diff < 0 {
				diff = -diff
			}
			if diff > clusterTimeWindow {
				continue
			}
			// Compared against the cluster's seed item only (not every
			// existing member): this avoids transitive drift, where A~B and
			// B~C get chained into a cluster even though A and C aren't
			// actually related.
			if jaccardSimilarity(tokenSets[i], tokenSets[j]) >= clusterSimThreshold {
				cl.items = append(cl.items, cands[j])
				assigned[j] = true
			}
		}
		clusters = append(clusters, cl)
	}
	return clusters
}

func sameFeedAsAny(items []candidate, feedID uuid.UUID) bool {
	for _, it := range items {
		if it.feed.ID == feedID {
			return true
		}
	}
	return false
}

// titleStopwords are short, extremely common Bangla connective words that
// would otherwise inflate similarity between unrelated headlines just
// because both are grammatical Bangla sentences.
var titleStopwords = map[string]bool{
	"এবং": true, "করে": true, "করেছে": true, "করেছেন": true, "হয়েছে": true,
	"হয়েছেন": true, "নিয়ে": true, "সঙ্গে": true, "পর": true, "এই": true,
	"সেই": true, "তার": true, "তাদের": true, "কে": true, "তে": true,
	"এর": true, "জন্য": true, "থেকে": true, "হলো": true, "হবে": true,
	"দিয়ে": true, "আর": true, "যে": true, "এক": true, "will": true,
	"the": true, "and": true, "for": true, "with": true, "says": true,
}

// titleTokens normalizes a headline into a set of lowercase word tokens for
// Jaccard comparison: splits on anything that isn't a letter/mark/number
// (Unicode-aware, so Bangla conjuncts are handled correctly), drops very
// short tokens and the stopword list above.
func titleTokens(title string) map[string]bool {
	out := make(map[string]bool)
	for _, tok := range strings.FieldsFunc(strings.ToLower(title), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsMark(r) && !unicode.IsNumber(r)
	}) {
		if len([]rune(tok)) < 2 || titleStopwords[tok] {
			continue
		}
		out[tok] = true
	}
	return out
}

func jaccardSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
