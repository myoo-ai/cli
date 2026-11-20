package thesaurus

import (
	"slices"
	"strings"
)

// Language describes a supported language. It says where we get its sources
// and how we reduce its words to base forms.
type Language struct {
	// Code is the ISO 639-1 code you pass to --lang.
	Code string
	Name string
	// path of the MyThes file in the LibreOffice dictionaries repository
	thesaurus string
	// prefixes and suffixes are inflection affixes the lemmatizer strips.
	// We append endings to the resulting stems to get lemma candidates.
	prefixes, suffixes, endings []string
	stopwords                   map[string]bool
	// auxiliaries come before a past participle used as a verb in perfect
	// tenses.
	auxiliaries map[string]bool
	// freqFix repairs characters we know are mis-encoded in the frequency
	// list.
	freqFix *strings.Replacer
}

// IsStopword reports whether the case-folded word is a function word, which
// we shouldn't rewrite or list as a root.
func (l *Language) IsStopword(word string) bool { return l.stopwords[word] }

func lang(code, name, thesaurus, prefixes, suffixes, endings string) *Language {
	return &Language{
		Code: code, Name: name, thesaurus: thesaurus,
		prefixes:    strings.Fields(prefixes),
		suffixes:    strings.Fields(suffixes),
		endings:     strings.Fields(endings),
		stopwords:   wordSet(stopwords[code]),
		auxiliaries: wordSet(perfectAuxiliaries[code]),
	}
}

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for w := range strings.FieldsSeq(s) {
		m[w] = true
	}
	return m
}

// languages lists the languages that have both a thesaurus and a frequency
// list. We leave out Latvian (stub thesaurus) and Esperanto (no frequency
// list).
var languages = []*Language{
	lang("ar", "Arabic", "ar/th_ar.dat",
		"وال بال كال فال لل ال و ف ب ل ك",
		"ات ون ين ان ها هم هن كم نا ة ه ي ك", "ة"),
	lang("bg", "Bulgarian", "bg_BG/th_bg_BG_v2.dat", "",
		"ата ото ите ове еве ия аха ала али яме ът ят та то те ах ал ям ем еш ат и а я о е",
		"а я о е ам ям"),
	lang("ca", "Catalan", "ca/dictionaries/th_ca_ES_v3.dat", "",
		"s es os ats ades at ada ant ent int ava aven en a o",
		"ar er ir re a e"),
	lang("cs", "Czech", "cs_CZ/thes_cs_CZ.dat", "",
		"ového ovému ových ovými ého ému ých ými ami ech ům ou em ím ila il ilo ili ali al ala alo uje ují a y i u e o ů í á é",
		"a o e ý í t at it ovat"),
	lang("da", "Danish", "da_DK/th_da_DK.dat", "",
		"erne ene ens ets ede en et er t de te s e",
		"e"),
	lang("de", "German", "de/th_de_DE_v2.dat", "ge",
		"sten ste ern ten tes ter est en er es em et te st e n s t",
		"en e n"),
	lang("en", "English", "en/th_en_US_v2.dat", "", "", ""),
	lang("es", "Spanish", "es/th_es_v2.dat", "",
		"ados adas idos idas ando iendo aban abas aron ieron amos emos imos ado ada ido ida aba ían ía ces es os as an en ió ó é s a o",
		"o a ar er ir z"),
	lang("fr", "French", "fr_FR/dictionaries/thes_fr.dat", "",
		"aient èrent eaux ées ée és ait ais ant ons irons ira ient aux ez er ent es é ie ies is it ai a e s x",
		"e er ir re al eau"),
	lang("gl", "Galician", "gl/thesaurus_gl.dat", "",
		"ados adas idos idas ando endo indo avan ado ada ido ida ava ou am em óns ais eis es os as s",
		"o a ar er ir ón al el"),
	lang("hu", "Hungarian", "hu_HU/th_hu_HU_v2.dat", "",
		"jaink jeink ainak einek ban ben ból ből nak nek val vel hoz hez höz ról ről juk jük ok ek ök ak at et ot öt ra re on en ön ja je ba be ás és t n k i",
		"a e"),
	lang("id", "Indonesian", "id/th_id_ID_v2.dat",
		"meng meny mem men me ber di ter per pe ke se",
		"nya kan an i lah kah", ""),
	lang("is", "Icelandic", "is/th_is.dat", "",
		"unum anna inn ana una ið in ar ir ur um a i u s",
		"a ur i ir"),
	lang("it", "Italian", "it_IT/th_it_IT_v2.dat", "",
		"avano iamo ando endo ato ata ati ate ito ita iti ite uto uta uti ute ava ano ono ò ì i e a o",
		"o a e are ere ire"),
	lang("no", "Norwegian (Bokmål)", "no/th_nb_NO_v2.dat", "",
		"ene ens ets ede en et er t de te s e",
		"e"),
	lang("pl", "Polish", "pl_PL/th_pl_PL_v2.dat", "",
		"ami ach ego emu ych ymi ów om em ie ym ej ał ała ali ało ła li a y i ę ą u e o",
		"a o e ć ać ić eć y i"),
	lang("pt", "Portuguese", "pt_PT/th_pt_PT.dat", "",
		"ados adas idos idas ando endo indo avam ado ada ido ida ava ões ais eis ou am em es os as s",
		"o a ar er ir ão al el"),
	lang("ro", "Romanian", "ro/th_ro_RO_v2.dat", "",
		"urile ilor ului ește ează lor ele uri ați esc ul ii le ei at ată ate i e a ă",
		"a ă e i ie re ui"),
	lang("ru", "Russian", "ru_RU/th_ru_RU_M_aot_and_v2.dat", "",
		"ться ами ями ого его ому ему ала али ешь ишь ете ите ов ев ей ам ям ах ях ом ем ой ою ую ая яя ое ее ые ие ым им ых их ет ют ит ат ят ал ло ла ли ут ся сь а я ы и у ю е о",
		"а я ь о е ый ий ой ть ться"),
	lang("sk", "Slovak", "sk_SK/th_sk_SK_v2.dat", "",
		"ového ovému ových ovými ého ému ých ými ami ách och om ou ím ila il ilo ili ali al ala alo uje ujú a y i u e o í á é",
		"a o e ý í ť ať iť ovať"),
	lang("sl", "Slovenian", "sl_SI/th_sl_SI_v2.dat", "",
		"ega emu ami ila ilo ali ala om em ih im ov ev il al a e i o u",
		"a o e ti ati iti"),
	lang("sv", "Swedish", "sv_SE/th_sv_SE.dat", "",
		"erna arna orna ens ets en et er ar or na ne de te t s",
		"a e"),
	lang("uk", "Ukrainian", "uk_UA/th_uk_UA.dat", "",
		"тися ами ями ого ому ала али ів їв ей ам ям ах ях ом ем ою ую ий ій ої их ими ає ють ить ать ять ав ло ла ли ся а я и і у ю е о",
		"а я ь о е ий ій ти тися"),
}

func init() {
	for _, l := range languages {
		if l.Code == "is" {
			// The Icelandic list spells eth as d with stroke.
			l.freqFix = strings.NewReplacer("đ", "ð")
		}
	}
}

// Lookup returns the language with the given code.
func Lookup(code string) (*Language, bool) {
	i := slices.IndexFunc(languages, func(l *Language) bool { return l.Code == code })
	if i < 0 {
		return nil, false
	}
	return languages[i], true
}

// Codes returns the codes of the supported languages in alphabetical order.
func Codes() []string {
	codes := make([]string, len(languages))
	for i, l := range languages {
		codes[i] = l.Code
	}
	slices.Sort(codes)
	return codes
}
