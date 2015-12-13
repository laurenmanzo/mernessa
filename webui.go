/*
	This Go Package responds to any request by sending a response containing the message Hello, vanessa.

*/

package webui

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"sync"

	"backend"

	"appengine"

	"github.com/laktek/Stack-on-Go/stackongo"
)

// Functions for sorting
type byCreationDate []stackongo.Question

func (a byCreationDate) Len() int           { return len(a) }
func (a byCreationDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byCreationDate) Less(i, j int) bool { return a[i].Creation_date > a[j].Creation_date }

// Reply to send to template
type genReply struct {
	Wrapper *stackongo.Questions   // Information about the query
	Caches  []cacheInfo            // Slice of the 4 caches (Unanswered, Answered, Pending, Updating)
	User    stackongo.User         // Information on the current user
	Qns     map[int]stackongo.User // Map of users by question ids
}

// Info on the various caches
type cacheInfo struct {
	CacheType string               // "unanswered"/"answered"/"pending"/"updating"
	Questions []stackongo.Question // list of questions
	Info      string               // blurb about the cache
}

type webData struct {
	lastUpdateTime  int64                // Time the cache was last updated in Unix
	wrapper         *stackongo.Questions // Request information
	unansweredCache []stackongo.Question // Unanswered questions
	answeredCache   []stackongo.Question // Answered questions
	pendingCache    []stackongo.Question // Pending questions
	updatingCache   []stackongo.Question // Updating questions
	cacheLock       sync.Mutex           // For multithreading, will use to avoid updating cache and serving cache at the same time
}

type userData struct {
	user_info     stackongo.User       // SE user info
	access_token  string               // Token to access info
	answeredCache []stackongo.Question // Questions answered by user
	pendingCache  []stackongo.Question // Questions being answered by user
	updatingCache []stackongo.Question // Questions that are being updated
}

// Global variable with cache info
var data = webData{}

// Map of users by user ids
var users = make(map[int]*userData)

// Map relating question ids to users
var qns = make(map[int]stackongo.User)

// Standard guest user
var guest = stackongo.User{
	Display_name: "guest",
}

// Functions for template to recieve data from maps
func (r genReply) GetUserID(id int) int {
	return r.Qns[id].User_id
}
func (r genReply) GetUserName(id int) string {
	return r.Qns[id].Display_name
}

//The app engine will run its own main function and imports this code as a package
//So no main needs to be defined
//All routes go in to init
func init() {
	// TODO(gregoriou): Comment out when ready to request from stackoverflow
	input, err := ioutil.ReadFile("3-12_dataset.json") // Read from most recent file
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	data.wrapper = new(stackongo.Questions) // Create a new wrapper

	// Unmarshal input from file into structs
	if err := json.Unmarshal(input, data.wrapper); err != nil {
		fmt.Println(err.Error())
		return
	}
	data.unansweredCache = data.wrapper.Items // At start, all questions are unanswered

	http.HandleFunc("/login", authHandler)
	http.HandleFunc("/", handler)
	http.HandleFunc("/tag", handler)
	http.HandleFunc("/user", handler)
}

// Handler for authorizing user
func authHandler(w http.ResponseWriter, r *http.Request) {
	auth_url := backend.AuthURL()
	header := w.Header()
	header.Add("Location", auth_url)
	w.WriteHeader(302)
}

// Handler for main information to be read and written from
func handler(w http.ResponseWriter, r *http.Request) {
	// Create a new appengine context for logging purposes
	c := appengine.NewContext(r)

	backend.SetTransport(r)
	_ = backend.NewSession(r)

	// Collect access token from browswer cookie
	// If cookie does not exist, obtain token using code from URL and set as cookie
	// If code does not exist, redirect to login page for authorization
	token, err := r.Cookie("access_token")
	var access_tokens map[string]string
	if err != nil {
		code := r.URL.Query().Get("code")
		if code != "" {
			c.Infof("Getting new user code")
			handler(w, r)
			return
		}
		access_tokens, err = backend.ObtainAccessToken(code)
		if err == nil {
			c.Infof("Setting cookie: access_token")
			http.SetCookie(w, &http.Cookie{Name: "access_token", Value: access_tokens["access_token"]})
		} else {
			c.Errorf(err.Error())
			errorHandler(w, r, 0, err.Error())
			return
		}
	}

	user, err := backend.AuthenticatedUser(map[string]string{}, token.Value)
	if err != nil {
		c.Errorf(err.Error())
		errorHandler(w, r, 0, err.Error())
		return
	}

	// update the new cache on submit
	data.cacheLock.Lock()
	cookie, _ := r.Cookie("submitting")
	if cookie != nil {
		if cookie.Value == "true" {
			err = updatingCache_User(r, c, user)
			if err != nil {
				c.Errorf(err.Error())
			}
			http.SetCookie(w, &http.Cookie{Name: "submitting", Value: ""})
		}
	}
	data.cacheLock.Unlock()

	// Send to tag subpage
	if r.URL.Path == "/tag" && r.FormValue("q") != "" {
		tagHandler(w, r, c, user)
		return
	}

	// Send to user subpage
	if r.URL.Path == "/user" {
		userHandler(w, r, c, user)
		return
	}

	page := template.Must(template.ParseFiles("public/template.html"))
	// WriteResponse creates a new response with the various caches
	if err := page.Execute(w, writeResponse(user, data.unansweredCache, data.answeredCache, data.pendingCache, data.updatingCache)); err != nil {
		c.Criticalf("%v", err.Error())
	}

}

// Handler to find all questions with specific tags
func tagHandler(w http.ResponseWriter, r *http.Request, c appengine.Context, user stackongo.User) {
	// Collect query
	tag := r.FormValue("q")

	// Create and fill in a new webData struct
	tempData := webData{}

	// range through the question caches golang stackongoand add if the question contains the tag
	for _, question := range data.unansweredCache {
		if contains(question.Tags, tag) {
			tempData.unansweredCache = append(tempData.unansweredCache, question)
		}
	}
	for _, question := range data.answeredCache {
		if contains(question.Tags, tag) {
			tempData.answeredCache = append(tempData.answeredCache, question)
		}
	}
	for _, question := range data.pendingCache {
		if contains(question.Tags, tag) {
			tempData.pendingCache = append(tempData.pendingCache, question)
		}
	}
	for _, question := range data.updatingCache {
		if contains(question.Tags, tag) {
			tempData.updatingCache = append(tempData.updatingCache, question)
		}
	}

	page := template.Must(template.ParseFiles("public/template.html"))
	if err := page.Execute(w, writeResponse(user, tempData.unansweredCache, tempData.answeredCache, tempData.pendingCache, tempData.updatingCache)); err != nil {
		c.Criticalf("%v", err.Error())
	}
}

// Handler to find all questions answered/being answered by the user in URL
func userHandler(w http.ResponseWriter, r *http.Request, c appengine.Context, user stackongo.User) {
	userID, _ := strconv.Atoi(r.FormValue("id"))

	page := template.Must(template.ParseFiles("public/template.html"))

	if _, ok := users[userID]; !ok {
		page.Execute(w, writeResponse(user, nil, nil, nil, nil))
		return
	}
	if err := page.Execute(w, writeResponse(user, nil, users[userID].answeredCache, users[userID].pendingCache, users[userID].updatingCache)); err != nil {
		c.Criticalf("%v", err.Error())
	}
}

// Write a genReply struct with the inputted Question slices
func writeResponse(user stackongo.User, unanswered []stackongo.Question, answered []stackongo.Question, pending []stackongo.Question, updating []stackongo.Question) genReply {
	return genReply{
		Wrapper: data.wrapper, // The global wrapper
		Caches: []cacheInfo{ // Slices caches and their relevant info
			cacheInfo{
				CacheType: "unanswered",
				Questions: unanswered,
				Info:      "These are questions that have not yet been answered by the Places API team",
			},
			cacheInfo{
				CacheType: "answered",
				Questions: answered,
				Info:      "These are questions that have been answered by the Places API team",
			},
			cacheInfo{
				CacheType: "pending",
				Questions: pending,
				Info:      "These are questions that are being answered by the Places API team",
			},
			cacheInfo{
				CacheType: "updating",
				Questions: updating,
				Info:      "These are questions that will be answered in the next release",
			},
		},
		User: user, // Current user information
		Qns:  qns,  // Map users by questions answered
	}
}

// updating the caches based on input from the app
func updatingCache_User(r *http.Request, c appengine.Context, user stackongo.User) error {
	c.Infof("updating cache")
	if true /* time on sql db is later than lastUpdatedTime */ {
		// Don't update
		// send error
	}
	// required to collect post form data
	r.ParseForm()

	// If the user is not in the database, add a new entry
	if _, ok := users[user.User_id]; !ok {
		users[user.User_id] = &userData{}
		users[user.User_id].init(user, "")
	}

	tempData := webData{}

	// Collect the submitted form info based on the name of the form
	for _, question := range data.unansweredCache {
		name := "unanswered_" + strconv.Itoa(question.Question_id)
		form_input := r.PostFormValue(name)
		switch form_input {
		case "unanswered":
			tempData.unansweredCache = append(tempData.unansweredCache, question)
		case "answered":
			tempData.answeredCache = append(tempData.answeredCache, question)
			users[user.User_id].answeredCache = append(users[user.User_id].answeredCache, question)
		case "pending":
			tempData.pendingCache = append(tempData.pendingCache, question)
			users[user.User_id].pendingCache = append(users[user.User_id].pendingCache, question)
		case "updating":
			tempData.updatingCache = append(tempData.updatingCache, question)
			users[user.User_id].updatingCache = append(users[user.User_id].updatingCache, question)
		case "no_change":
			tempData.unansweredCache = append(tempData.unansweredCache, question)
		}

		// Map the user to the question if the question is done
		if form_input != "no_change" && form_input != "unanswered" {
			qns[question.Question_id] = user
		}
	}

	for _, question := range data.answeredCache {
		name := "answered_" + strconv.Itoa(question.Question_id)
		form_input := r.PostFormValue(name)
		switch form_input {
		case "unanswered":
			tempData.unansweredCache = append(tempData.unansweredCache, question)
		case "answered":
			tempData.answeredCache = append(tempData.answeredCache, question)
			users[user.User_id].answeredCache = append(users[user.User_id].answeredCache, question)
		case "pending":
			tempData.pendingCache = append(tempData.pendingCache, question)
			users[user.User_id].pendingCache = append(users[user.User_id].pendingCache, question)
		case "updating":
			tempData.updatingCache = append(tempData.updatingCache, question)
			users[user.User_id].updatingCache = append(users[user.User_id].updatingCache, question)
		case "no_change":
			tempData.answeredCache = append(tempData.answeredCache, question)
		}

		// If the question is now unanswered, delete question from map
		if form_input == "unanswered" {
			qns[question.Question_id] = stackongo.User{}
			delete(qns, question.Question_id)

			// Else remove question from original editor's cache and map user to question
		} else if form_input != "no_change" {

			editor := qns[question.Question_id]
			for i, q := range users[editor.User_id].answeredCache {
				if question.Question_id == q.Question_id {
					users[editor.User_id].answeredCache = append(users[editor.User_id].answeredCache[:i], users[editor.User_id].answeredCache[i+1:]...)
					break
				}
			}

			qns[question.Question_id] = user
		}
	}

	for _, question := range data.pendingCache {
		name := "pending_" + strconv.Itoa(question.Question_id)
		form_input := r.PostFormValue(name)
		switch form_input {
		case "unanswered":
			tempData.unansweredCache = append(tempData.unansweredCache, question)
		case "answered":
			tempData.answeredCache = append(tempData.answeredCache, question)
			users[user.User_id].answeredCache = append(users[user.User_id].answeredCache, question)
		case "pending":
			tempData.pendingCache = append(tempData.pendingCache, question)
			users[user.User_id].pendingCache = append(users[user.User_id].pendingCache, question)
		case "updating":
			tempData.updatingCache = append(tempData.updatingCache, question)
			users[user.User_id].updatingCache = append(users[user.User_id].updatingCache, question)
		case "no_change":
			tempData.pendingCache = append(tempData.pendingCache, question)
		}

		// If the question is now unanswered, delete question from map
		if form_input == "unanswered" {
			qns[question.Question_id] = stackongo.User{}
			delete(qns, question.Question_id)

			// Else remove question from original editor's cache and map user to question
		} else if form_input != "no_change" {

			editor := qns[question.Question_id]
			for i, q := range users[editor.User_id].pendingCache {
				if question.Question_id == q.Question_id {
					users[editor.User_id].pendingCache = append(users[editor.User_id].pendingCache[:i], users[editor.User_id].pendingCache[i+1:]...)
					break
				}
			}

			qns[question.Question_id] = user
		}
	}

	for _, question := range data.updatingCache {
		name := "updating_" + strconv.Itoa(question.Question_id)
		form_input := r.PostFormValue(name)
		switch form_input {
		case "unanswered":
			tempData.unansweredCache = append(tempData.unansweredCache, question)
		case "answered":
			tempData.answeredCache = append(tempData.answeredCache, question)
			users[user.User_id].answeredCache = append(users[user.User_id].answeredCache, question)
		case "pending":
			tempData.pendingCache = append(tempData.pendingCache, question)
			users[user.User_id].pendingCache = append(users[user.User_id].pendingCache, question)
		case "updating":
			tempData.updatingCache = append(tempData.updatingCache, question)
			users[user.User_id].updatingCache = append(users[user.User_id].updatingCache, question)
		case "no_change":
			tempData.updatingCache = append(tempData.updatingCache, question)
		}

		// If the question is now unanswered, delete question from map
		if form_input == "unanswered" {
			qns[question.Question_id] = stackongo.User{}
			delete(qns, question.Question_id)

			// Else remove question from original editor's cache and map user to question
		} else if form_input != "no_change" {

			editor := qns[question.Question_id]
			for i, q := range users[editor.User_id].updatingCache {
				if question.Question_id == q.Question_id {
					users[editor.User_id].updatingCache = append(users[editor.User_id].updatingCache[:i], users[editor.User_id].updatingCache[i+1:]...)
					break
				}
			}

			qns[question.Question_id] = user
		}
	}

	// sort caches by creation date
	sort.Stable(byCreationDate(tempData.unansweredCache))
	sort.Stable(byCreationDate(tempData.answeredCache))
	sort.Stable(byCreationDate(tempData.pendingCache))
	sort.Stable(byCreationDate(tempData.updatingCache))

	// replace global caches with new caches
	data.unansweredCache = tempData.unansweredCache
	data.answeredCache = tempData.answeredCache
	data.pendingCache = tempData.pendingCache
	data.updatingCache = tempData.updatingCache

	// sort user caches by creation date
	sort.Stable(byCreationDate(users[user.User_id].answeredCache))
	sort.Stable(byCreationDate(users[user.User_id].pendingCache))
	sort.Stable(byCreationDate(users[user.User_id].updatingCache))

	/* change lastUpdatedTime and time on db */
	return nil
}

// Handler for errors
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

// Returns true if toFind is an element of slice
func contains(slice []string, toFind string) bool {
	for _, tag := range slice {
		if reflect.DeepEqual(tag, toFind) {
			return true
		}
	}
	return false
}

// Initializes userData struct
func (user userData) init(u stackongo.User, token string) {
	user.user_info = u
	user.access_token = token
	user.answeredCache = []stackongo.Question{}
	user.pendingCache = []stackongo.Question{}
	user.updatingCache = []stackongo.Question{}
}
