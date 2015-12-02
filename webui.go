/*
	This Go Package responds to any request by sending a response containing the message Hello, vanessa.

*/

package webui

import (
	"encoding/json"
	"html/template"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/laktek/Stack-on-Go/stackongo"
)

type reply struct {
	Wrapper         *stackongo.Questions
	UnansweredReply []stackongo.Question
	AnsweredReply   []stackongo.Question
	PendingReply    []stackongo.Question
	UpdatingReply   []stackongo.Question
	FindQuery       string
}

type webData struct {
	wrapper         *stackongo.Questions
	unansweredCache []stackongo.Question
	answeredCache   []stackongo.Question
	pendingCache    []stackongo.Question
	updateCache     []stackongo.Question
	cacheLock       sync.Mutex
}

var data = webData{}

//The app engine will run its own main function and imports this code as a package
//So no main needs to be defined
//All routes go in to init
func init() {
	// TODO(gregoriou): Comment out when ready to request from stackoverflow
	input, err := ioutil.ReadFile("27-11_dataset.json")
	if err != nil {
		return
	}
	data.wrapper = new(stackongo.Questions)
	if err := json.Unmarshal(input, data.wrapper); err != nil {
		return
	}
	data.unansweredCache = data.wrapper.Items

	http.HandleFunc("/", handler)
}

func handler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		errorHandler(w, r, http.StatusNotFound, "")
		return
	}

	page := template.Must(template.ParseFiles("public/template.html"))

	// TODO(gregoriou): Uncomment when ready to request from stackoverflow
	/*
		input, err := dataCollect.Collect(r)
		if err != nil {
			fmt.Fprintf(w, "%v\n", err.Error())
			return
		}
	*/

	data.updateCache_User(r)

	response := reply{
		Wrapper:         data.wrapper,
		UnansweredReply: data.unansweredCache,
		AnsweredReply:   data.answeredCache,
		PendingReply:    data.pendingCache,
		UpdatingReply:   data.updateCache,
		FindQuery:       "",
	}
	if err := page.Execute(w, response); err != nil {
		panic(err)
	}
}

// Updates the caches based on input from the app
func (w webData) updateCache_User(r *http.Request) {
	r.ParseForm()

	tempData := webData{}
	for i, question := range data.unansweredCache {
		tag := "unanswered_state"
		tag = strings.Join([]string{tag, strconv.Itoa(i)}, "")
		form_input := r.PostFormValue(tag)
		switch form_input {
		case "answered":
			tempData.answeredCache = append(tempData.answeredCache, question)
		case "pending":
			tempData.pendingCache = append(tempData.pendingCache, question)
		case "updating":
			tempData.updateCache = append(tempData.updateCache, question)
		default:
			tempData.unansweredCache = append(tempData.unansweredCache, question)
		}
	}

	for i, question := range data.answeredCache {
		tag := "answered_state"
		tag = strings.Join([]string{tag, strconv.Itoa(i)}, "")
		form_input := r.PostFormValue(tag)
		switch form_input {
		case "answered":
			tempData.answeredCache = append(tempData.answeredCache, question)
		case "pending":
			tempData.pendingCache = append(tempData.pendingCache, question)
		case "updating":
			tempData.updateCache = append(tempData.updateCache, question)
		default:
			tempData.unansweredCache = append(tempData.unansweredCache, question)
		}
	}

	for i, question := range data.pendingCache {
		tag := "pending_state"
		tag = strings.Join([]string{tag, strconv.Itoa(i)}, "")
		form_input := r.PostFormValue(tag)
		switch form_input {
		case "answered":
			tempData.answeredCache = append(tempData.answeredCache, question)
		case "pending":
			tempData.pendingCache = append(tempData.pendingCache, question)
		case "updating":
			tempData.updateCache = append(tempData.updateCache, question)
		default:
			tempData.unansweredCache = append(tempData.unansweredCache, question)
		}
	}

	for i, question := range data.updateCache {
		tag := "update_state"
		tag = strings.Join([]string{tag, strconv.Itoa(i)}, "")
		form_input := r.PostFormValue(tag)
		switch form_input {
		case "answered":
			tempData.answeredCache = append(tempData.answeredCache, question)
		case "pending":
			tempData.pendingCache = append(tempData.pendingCache, question)
		case "updating":
			tempData.updateCache = append(tempData.updateCache, question)
		default:
			tempData.unansweredCache = append(tempData.unansweredCache, question)
		}
	}

	data.unansweredCache = tempData.unansweredCache
	data.answeredCache = tempData.answeredCache
	data.pendingCache = tempData.pendingCache
	data.updateCache = tempData.updateCache
}

func errorHandler(w http.ResponseWriter, r *http.Request, status int, err string) {
	w.WriteHeader(status)
	switch status {
	case http.StatusNotFound:
		page := template.Must(template.ParseFiles("public/404.html"))
		if err := page.Execute(w, nil); err != nil {
			errorHandler(w, r, http.StatusInternalServerError, err.Error())
			return
		}
	}
}
