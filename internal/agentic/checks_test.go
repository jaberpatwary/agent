package agentic

import (
	"strings"
	"testing"
)

const origStory = `<p>ঢাকার মিরপুরে বৃহস্পতিবার সকালে একটি পোশাক কারখানায় ভয়াবহ আগুন লাগে বলে জানিয়েছে ফায়ার সার্ভিস।</p>
<p>ফায়ার সার্ভিসের ১২টি ইউনিট প্রায় দুই ঘণ্টার চেষ্টায় আগুন নিয়ন্ত্রণে আনে এবং এতে অন্তত ৪৫ জন শ্রমিক আহত হন।</p>
<p>কারখানার মালিকপক্ষ জানিয়েছে, আগুনে প্রায় ৩০০ কোটি টাকার ক্ষতি হয়েছে এবং তদন্ত কমিটি গঠন করা হয়েছে।</p>`

func TestCheckDraftAcceptsAFullOriginalRewrite(t *testing.T) {
	draft := `<p>রাজধানীর মিরপুর এলাকায় বৃহস্পতিবার সকালের দিকে এক পোশাক তৈরির কারখানায় বড় ধরনের অগ্নিকাণ্ড ঘটেছে, ফায়ার সার্ভিস এ তথ্য নিশ্চিত করেছে।</p>
<p>আগুন নেভাতে ফায়ার সার্ভিসের ১২টি দল প্রায় দুই ঘণ্টা কাজ করে, এ ঘটনায় কমপক্ষে ৪৫ জন শ্রমিক আহত হয়েছেন বলে জানা গেছে।</p>
<p>কারখানা কর্তৃপক্ষের হিসাবে ক্ষতির পরিমাণ আনুমানিক ৩০০ কোটি টাকা, আর ঘটনার কারণ খুঁজতে একটি তদন্ত কমিটি করা হয়েছে।</p>`
	c := CheckDraft("মিরপুরে আগুন", origStory, draft, nil)
	if !c.OK() {
		t.Fatalf("a full, rewritten draft should pass, problems: %v", c.Problems)
	}
}

func TestCheckDraftCatchesShortCopiedAndAttributedDrafts(t *testing.T) {
	short := `<p>মিরপুরে আগুনে ৪৫ জন আহত।</p>`
	if c := CheckDraft("t", origStory, short, nil); c.OK() || !hasProblem(c, "not the full story") {
		t.Fatalf("a summary must fail the length rule: %v", c.Problems)
	}

	copied := origStory + `<p>জাগো নিউজকে এক কর্মকর্তা এসব তথ্য জানান।</p>`
	c := CheckDraft("t", origStory, copied, nil)
	if !hasProblem(c, "copied word for word") {
		t.Fatalf("verbatim copy must be caught: %v", c.Problems)
	}
	if !hasProblem(c, "mentions a news outlet") {
		t.Fatalf("outlet name must be caught: %v", c.Problems)
	}

	noNumbers := strings.NewReplacer("১২টি", "অনেকগুলো", "৪৫", "বেশ কয়েকজন", "৩০০", "বিপুল").Replace(
		`<p>রাজধানীর মিরপুর এলাকায় বৃহস্পতিবার সকালের দিকে এক পোশাক তৈরির কারখানায় বড় ধরনের অগ্নিকাণ্ড ঘটেছে, ফায়ার সার্ভিস এ তথ্য নিশ্চিত করেছে।</p>
<p>আগুন নেভাতে ফায়ার সার্ভিসের ১২টি দল প্রায় দুই ঘণ্টা কাজ করে, এ ঘটনায় কমপক্ষে ৪৫ জন শ্রমিক আহত হয়েছেন বলে জানা গেছে।</p>
<p>কারখানা কর্তৃপক্ষের হিসাবে ক্ষতির পরিমাণ আনুমানিক ৩০০ কোটি টাকা, আর ঘটনার কারণ খুঁজতে একটি তদন্ত কমিটি করা হয়েছে।</p>`)
	if c := CheckDraft("t", origStory, noNumbers, nil); !hasProblem(c, "numbers") {
		t.Fatalf("dropped figures must be caught: %v", c.Problems)
	}
}

func hasProblem(c DraftCheck, sub string) bool {
	for _, p := range c.Problems {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}
