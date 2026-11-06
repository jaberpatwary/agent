package ai

import (
	"fmt"
	"strings"
)

// SynthesisSource is one publisher's coverage of a real-world event that is
// being combined with others into a single multi-source article. See
// internal/agent.Rewriter.RewriteMulti.
type SynthesisSource struct {
	Title         string
	Content       string
	PublisherName string
}

// DefaultHouseStyle is used the first time the portal runs, before an admin
// customizes it from /admin/style. It is intentionally generic — a safe,
// professional Bangla news voice — and is meant to be edited, not kept
// forever.
const DefaultHouseStyle = `স্পষ্ট ও সহজবোধ্য প্রমিত বাংলায় লেখো। বাক্য ছোট ও সরাসরি রাখো, জটিল বা দুর্বোধ্য শব্দ এড়িয়ে চলো। ` +
	`নিরপেক্ষ সাংবাদিকতার সুর বজায় রাখো — ব্যক্তিগত মতামত, অতিরঞ্জন বা সংবেদনশীলতা (sensationalism) থেকে বিরত থাকো। ` +
	`প্রতিটি অনুচ্ছেদ ৩-৫ বাক্যের মধ্যে রাখো যাতে মোবাইলে পড়তে সুবিধা হয়।`

// RewriteSystemPrompt is the system role used for both the draft and the
// review step of the agentic rewrite. houseStyle is admin-editable (stored
// in app_settings, see internal/agent).
func RewriteSystemPrompt(houseStyle string) string {
	return fmt.Sprintf(
		"তুমি একজন অভিজ্ঞ বাংলা সংবাদ সম্পাদক। তোমার কাজ হলো অন্য সংবাদমাধ্যম থেকে আসা একটি খবরকে, "+
			"মূল তথ্য অবিকৃত রেখে, নিজের ভাষায় নতুন করে লেখা — যাতে এটি কপি-পেস্ট মনে না হয় এবং একটি নির্দিষ্ট সম্পাদকীয় স্টাইল অনুসরণ করে।\n\n"+
			"সম্পাদকীয় স্টাইল (house style), এই নির্দেশনা অক্ষরে অক্ষরে মেনে চলো:\n%s\n\n"+
			"কঠোর নিয়ম (এগুলো কখনো ভঙ্গ করবে না):\n"+
			"১. মূল খবরে নেই এমন কোনো তথ্য, সংখ্যা, নাম, উক্তি বা ঘটনা যোগ করবে না — এটি সবচেয়ে গুরুত্বপূর্ণ নিয়ম।\n"+
			"২. মূল খবরের কোনো গুরুত্বপূর্ণ তথ্য (সংখ্যা, নাম, তারিখ, স্থান) বাদ দেবে না বা পরিবর্তন করবে না।\n"+
			"৩. বাক্য গঠন ও শব্দচয়ন বদলে দাও, কিন্তু অর্থ অবিকল রাখো — এটা অনুবাদ নয়, পুনর্লিখন (rewrite)।\n"+
			"৪. নিজের কোনো মন্তব্য, বিশ্লেষণ বা উপসংহার যোগ করবে না।\n"+
			"৫. শুধু অনুরোধকৃত JSON ফরম্যাটে উত্তর দাও — কোনো ভূমিকা, ব্যাখ্যা বা মার্কডাউন কোড-ফেন্স ছাড়া।",
		houseStyle,
	)
}

// BuildRewriteDraftPrompt asks the model to produce a first draft rewrite of
// the scraped article as strict JSON: {"title":..., "body":..., "summary":...}.
// body must be HTML paragraphs (<p>...</p>) because that is the format the
// rest of the pipeline (sanitizeArticleHTML at render time) already expects
// from scraped article content.
func BuildRewriteDraftPrompt(title, content string) string {
	return fmt.Sprintf(
		"নিচে একটি মূল সংবাদের শিরোনাম ও বিষয়বস্তু দেওয়া হলো। এটিকে উপরের নিয়ম অনুযায়ী নতুন করে লেখো।\n\n"+
			"মূল শিরোনাম: %s\n\n"+
			"মূল বিষয়বস্তু:\n%s\n\n"+
			"নিচের কাঠামোয়, শুধুমাত্র বৈধ JSON আকারে উত্তর দাও (অন্য কিছু লিখো না):\n"+
			"{\n"+
			"  \"title\": \"নতুন, আকর্ষণীয় কিন্তু সঠিক শিরোনাম (একটি বাক্য)\",\n"+
			"  \"body\": \"নতুন করে লেখা পুরো সংবাদ, প্রতিটি অনুচ্ছেদ <p> ট্যাগে মোড়ানো, যেমন: <p>প্রথম অনুচ্ছেদ...</p><p>দ্বিতীয় অনুচ্ছেদ...</p>\",\n"+
			"  \"summary\": \"৩-৪ বাক্যের সংক্ষিপ্ত সারাংশ (plain text, কোনো HTML ট্যাগ ছাড়া)\",\n"+
			"  \"tags\": [\"৩ থেকে ৫টি ছোট, নির্দিষ্ট বিষয়ভিত্তিক ট্যাগ — ব্যক্তি/স্থান/প্রতিষ্ঠান/নির্দিষ্ট বিষয়ের নাম, সাধারণ শব্দ নয়\"]\n"+
			"}",
		title, content,
	)
}

// BuildRewriteReviewPrompt is the self-review ("agentic") step: the model is
// handed its own draft back alongside the original source text and asked to
// fact-check and polish it before the final version is saved. This is a
// cheap, meaningful quality gate — not a substitute for human editorial
// review, which is why RunCronNow / the admin panel still let a human
// archive or delete anything that slips through.
func BuildRewriteReviewPrompt(title, content, draftJSON string) string {
	return fmt.Sprintf(
		"তুমি এখন একজন কঠোর সম্পাদক (editor) হিসেবে কাজ করছ, যে নিজের সহকারীর লেখা একটি খসড়া (draft) যাচাই করছে।\n\n"+
			"মূল শিরোনাম: %s\n\n"+
			"মূল বিষয়বস্তু (একমাত্র সত্য উৎস, এর বাইরে কিছু সত্য ধরা যাবে না):\n%s\n\n"+
			"সহকারীর লেখা খসড়া (JSON):\n%s\n\n"+
			"নিচের চেকলিস্ট অনুযায়ী যাচাই করো:\n"+
			"- খসড়ায় এমন কোনো তথ্য/সংখ্যা/নাম/উক্তি আছে কি যা মূল খবরে নেই? থাকলে বাদ দাও।\n"+
			"- মূল খবরের কোনো গুরুত্বপূর্ণ তথ্য বাদ পড়েছে বা ভুলভাবে উপস্থাপিত হয়েছে কি? থাকলে ঠিক করো।\n"+
			"- ভাষা কি নির্ধারিত সম্পাদকীয় স্টাইল মেনে চলছে, ব্যাকরণ ঠিক আছে, আর সত্যিই কপি-পেস্ট না হয়ে পুনর্লিখিত মনে হচ্ছে?\n"+
			"- body ঠিকমতো <p> ট্যাগে ভাগ করা আছে কি?\n\n"+
			"সংশোধন করে চূড়ান্ত (final) সংস্করণ একই JSON কাঠামোয় ফেরত দাও — শুধু বৈধ JSON, অন্য কিছু নয়:\n"+
			"{\"title\": \"...\", \"body\": \"...\", \"summary\": \"...\", \"tags\": [\"...\"]}",
		title, content, draftJSON,
	)
}

// BuildSynthesisDraftPrompt asks the model to combine several publishers'
// independent coverage of the same real-world event into one unified,
// cross-checked article — the multi-source counterpart of
// BuildRewriteDraftPrompt. This is the part of the pipeline that a plain
// "summarize this one article" aggregator cannot do: it requires reading
// several documents at once and reconciling them.
func BuildSynthesisDraftPrompt(sources []SynthesisSource) string {
	var b strings.Builder
	b.WriteString("নিচে একই বাস্তব ঘটনার উপর বিভিন্ন সংবাদমাধ্যমের আলাদা আলাদা প্রতিবেদন দেওয়া হলো। এগুলো একসাথে পড়ে, সবগুলো সূত্র মিলিয়ে একটিই সম্পূর্ণ ও নির্ভরযোগ্য প্রতিবেদন লেখো — এটি কোনো একটি সূত্রের অনুবাদ বা সারাংশ নয়, একাধিক সূত্রের সমন্বয়ে তৈরি নতুন লেখা।\n\n")
	for i, s := range sources {
		fmt.Fprintf(&b, "— সূত্র %d (%s) —\nশিরোনাম: %s\nবিষয়বস্তু:\n%s\n\n", i+1, s.PublisherName, s.Title, s.Content)
	}
	b.WriteString(
		"নির্দেশনা:\n" +
			"১. একাধিক সূত্রে থাকা তথ্যই মূল বিষয়বস্তুর কেন্দ্রে রাখো।\n" +
			"২. কোনো একটি সূত্রে থাকা গুরুত্বপূর্ণ বাড়তি তথ্যও অন্তর্ভুক্ত করতে পারো, যদি সেই সূত্রে তা স্পষ্টভাবে বলা থাকে — কিন্তু কোনো সূত্রেই নেই এমন কিছু নিজে থেকে যোগ করবে না।\n" +
			"৩. সূত্রগুলোর মধ্যে কোনো তথ্যে (যেমন সংখ্যা বা মৃতের সংখ্যা) গরমিল থাকলে, নিজে থেকে একটা বেছে নিয়ো না — হয় উভয় তথ্যই (সূত্র উল্লেখ করে) জানাও, অথবা অস্পষ্ট অংশটি বাদ দাও।\n" +
			"৪. লেখাটি হতে হবে একটিমাত্র প্রবাহমান প্রতিবেদন, আলাদা আলাদা অনুচ্ছেদে প্রতিটি সূত্র আলাদা করে বর্ণনা করবে না।\n\n")
	b.WriteString(
		"নিচের কাঠামোয়, শুধুমাত্র বৈধ JSON আকারে উত্তর দাও (অন্য কিছু লিখো না):\n" +
			"{\n" +
			"  \"title\": \"সম্পূর্ণ ঘটনা বোঝায় এমন একটি শিরোনাম\",\n" +
			"  \"body\": \"সম্পূর্ণ প্রতিবেদন, প্রতিটি অনুচ্ছেদ <p> ট্যাগে মোড়ানো\",\n" +
			"  \"summary\": \"৩-৪ বাক্যের সংক্ষিপ্ত সারাংশ (plain text)\",\n" +
			"  \"tags\": [\"৩ থেকে ৫টি ছোট, নির্দিষ্ট বিষয়ভিত্তিক ট্যাগ\"]\n" +
			"}")
	return b.String()
}

// BuildSynthesisReviewPrompt is the self-review step for a multi-source
// draft: check it against every original source, not just one.
func BuildSynthesisReviewPrompt(sources []SynthesisSource, draftJSON string) string {
	var b strings.Builder
	b.WriteString("তুমি একজন কঠোর সম্পাদক। নিচে একই ঘটনার উপর একাধিক মূল সূত্র এবং সেগুলো মিলিয়ে বানানো একটি সম্মিলিত (synthesized) খসড়া দেওয়া আছে।\n\n")
	for i, s := range sources {
		fmt.Fprintf(&b, "— সূত্র %d (%s) —\n%s\n\n", i+1, s.PublisherName, s.Content)
	}
	fmt.Fprintf(&b, "খসড়া (JSON):\n%s\n\n", draftJSON)
	b.WriteString(
		"নিচের চেকলিস্ট অনুযায়ী যাচাই করো:\n" +
			"- খসড়ায় এমন কোনো তথ্য আছে কি যা কোনো সূত্রেই নেই?\n" +
			"- কোনো সূত্রের গুরুত্বপূর্ণ তথ্য বাদ পড়েছে বা ভুলভাবে উপস্থাপিত হয়েছে কি?\n" +
			"- সূত্রগুলোর মধ্যে কোনো গরমিল ঠিকভাবে হ্যান্ডেল হয়েছে কি (একতরফাভাবে একটা বেছে নেওয়া হয়নি তো)?\n" +
			"- body ঠিকমতো <p> ট্যাগে ভাগ করা আছে কি?\n\n" +
			"সংশোধন করে চূড়ান্ত সংস্করণ একই JSON কাঠামোয় ফেরত দাও — শুধু বৈধ JSON, অন্য কিছু নয়:\n" +
			"{\"title\": \"...\", \"body\": \"...\", \"summary\": \"...\", \"tags\": [\"...\"]}")
	return b.String()
}
