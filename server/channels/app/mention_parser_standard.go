package app

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattermost/mattermost/server/public/model"
)

// Have the compiler confirm *StandardMentionParser implements MentionParser
var _ MentionParser = &StandardMentionParser{}

type StandardMentionParser struct {
	keywords MentionKeywords

	results *MentionResults
}

func makeStandardMentionParser(keywords MentionKeywords, groups map[string]*model.Group) *StandardMentionParser {
	// TODO add groups to keywords before this
	copiedKeywords := make(MentionKeywords)
	for keyword, ids := range keywords {
		copiedKeywords[keyword] = ids
	}
	for _, group := range groups {
		if group.Name != nil {
			copiedKeywords["@"+*group.Name] = append(copiedKeywords["@"+*group.Name], MentionableGroupID(group.Id))
		}
	}

	return &StandardMentionParser{
		keywords: copiedKeywords,

		results: &MentionResults{},
	}
}

func (p *StandardMentionParser) ProcessText(text string) {
	p.processText(text, p.keywords)
}

func (p *StandardMentionParser) Results() *MentionResults {
	return p.results
}

func (p *StandardMentionParser) processText(text string, keywords MentionKeywords) {
	systemMentions := map[string]bool{"@here": true, "@channel": true, "@all": true}

	for _, word := range strings.FieldsFunc(text, func(c rune) bool {
		// Split on any whitespace or punctuation that can't be part of an at mention or emoji pattern
		return !(c == ':' || c == '.' || c == '-' || c == '_' || c == '@' || unicode.IsLetter(c) || unicode.IsNumber(c))
	}) {
		// skip word with format ':word:' with an assumption that it is an emoji format only
		if word[0] == ':' && word[len(word)-1] == ':' {
			continue
		}

		word = strings.TrimLeft(word, ":.-_")

		if p.checkForMention(word, keywords) {
			continue
		}

		foundWithoutSuffix := false
		wordWithoutSuffix := word

		for wordWithoutSuffix != "" && strings.LastIndexAny(wordWithoutSuffix, ".-:_") == (len(wordWithoutSuffix)-1) {
			wordWithoutSuffix = wordWithoutSuffix[0 : len(wordWithoutSuffix)-1]

			if p.checkForMention(wordWithoutSuffix, keywords) {
				foundWithoutSuffix = true
				break
			}
		}

		if foundWithoutSuffix {
			continue
		}

		if _, ok := systemMentions[word]; !ok && strings.HasPrefix(word, "@") {
			// No need to bother about unicode as we are looking for ASCII characters.
			last := word[len(word)-1]
			switch last {
			// If the word is possibly at the end of a sentence, remove that character.
			case '.', '-', ':':
				word = word[:len(word)-1]
			}
			p.results.OtherPotentialMentions = append(p.results.OtherPotentialMentions, word[1:])
		} else if strings.ContainsAny(word, ".-:") {
			// This word contains a character that may be the end of a sentence, so split further
			splitWords := strings.FieldsFunc(word, func(c rune) bool {
				return c == '.' || c == '-' || c == ':'
			})

			for _, splitWord := range splitWords {
				if p.checkForMention(splitWord, keywords) {
					continue
				}
				if _, ok := systemMentions[splitWord]; !ok && strings.HasPrefix(splitWord, "@") {
					p.results.OtherPotentialMentions = append(p.results.OtherPotentialMentions, splitWord[1:])
				}
			}
		}

		if ids, match := isKeywordMultibyte(keywords, word); match {
			p.addMentions(ids, KeywordMention)
		}
	}
}

// checkForMention checks if there is a mention to a specific user or to the keywords here / channel / all
func (p *StandardMentionParser) checkForMention(word string, keywords MentionKeywords) bool {
	var mentionType MentionType

	switch strings.ToLower(word) {
	case "@here":
		p.results.HereMentioned = true
		mentionType = ChannelMention
	case "@channel":
		p.results.ChannelMentioned = true
		mentionType = ChannelMention
	case "@all":
		p.results.AllMentioned = true
		mentionType = ChannelMention
	default:
		mentionType = KeywordMention
	}

	if ids, match := keywords[strings.ToLower(word)]; match {
		p.addMentions(ids, mentionType)
		return true
	}

	// Case-sensitive check for first name
	if ids, match := keywords[word]; match {
		p.addMentions(ids, mentionType)
		return true
	}

	return false
}

func (p *StandardMentionParser) addMentions(ids []MentionableID, mentionType MentionType) {
	for _, id := range ids {
		if userID, ok := id.AsUserID(); ok {
			p.results.addMention(userID, mentionType)
		} else if groupID, ok := id.AsGroupID(); ok {
			p.results.addGroupMention(groupID)
		}
	}
}

// isKeywordMultibyte checks if a word containing a multibyte character contains a multibyte keyword
func isKeywordMultibyte(keywords MentionKeywords, word string) ([]MentionableID, bool) {
	ids := []MentionableID{}
	match := false
	var multibyteKeywords []string
	for keyword := range keywords {
		if len(keyword) != utf8.RuneCountInString(keyword) {
			multibyteKeywords = append(multibyteKeywords, keyword)
		}
	}

	if len(word) != utf8.RuneCountInString(word) {
		for _, key := range multibyteKeywords {
			if strings.Contains(word, key) {
				ids, match = keywords[key]
			}
		}
	}
	return ids, match
}
