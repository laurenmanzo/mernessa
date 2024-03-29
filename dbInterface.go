package webui

import (
	"backend"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"golang.org/x/net/context"

	"github.com/laktek/Stack-on-Go/stackongo"
	"google.golang.org/appengine/log"
)

// Returns questions and user data from the db filtered by parameters
func readFromDb(ctx context.Context, params string) (webData, int64, error) {
	log.Infof(ctx, "Refreshing database read")

	tempData := newWebData()
	var (
		url            string
		title          string
		id             int
		state          string
		body           string
		creation_date  int64
		last_edit_time sql.NullInt64
		owner          sql.NullInt64
		name           sql.NullString
		pic            sql.NullString
		link           sql.NullString
	)

	//Select all questions in the database and read into a new data object
	query := "SELECT * FROM questions LEFT JOIN user ON questions.user=user.id"
	if params != "" {
		query += " WHERE " + params
	}
	log.Infof(ctx, "query: %v", query)

	rows, err := db.Query(query)
	if err != nil {
		return tempData, 0, fmt.Errorf("query failed: %v", err.Error())
	}

	defer rows.Close()
	//Iterate through each row and add to the correct cache
	for rows.Next() {
		err := rows.Scan(&id, &title, &url, &state, &owner, &body, &creation_date, &last_edit_time, &owner, &name, &pic, &link)
		if err != nil {
			log.Errorf(ctx, "query failed: %v", err)
			continue
		}

		currentQ := stackongo.Question{
			Question_id:   id,
			Title:         title,
			Link:          url,
			Body:          body,
			Creation_date: creation_date,
		}
		if last_edit_time.Valid {
			currentQ.Last_edit_date = last_edit_time.Int64
		}

		var tagToAdd string
		//Get tags for that question, based on the ID
		tagRows, err := db.Query("SELECT tag FROM question_tag WHERE question_id = ?", currentQ.Question_id)
		if err != nil {
			log.Errorf(ctx, "Tag retrieval failed: %v", err.Error())
			continue
		}
		defer tagRows.Close()
		for tagRows.Next() {
			err := tagRows.Scan(&tagToAdd)
			if err != nil {
				log.Errorf(ctx, "Could not scan for tag: %v", err.Error())
				continue
			}
			currentQ.Tags = append(currentQ.Tags, tagToAdd)
		}
		//Switch on the state as read from the database to ensure question is added to correct cace
		tempData.Caches[state] = append(tempData.Caches[state], currentQ)

		if owner.Valid {
			user := stackongo.User{
				User_id:       int(owner.Int64),
				Display_name:  name.String,
				Profile_image: pic.String,
			}
			tempData.Qns[id] = user
			if _, ok := tempData.Users[user.User_id]; !ok {
				tempData.Users[user.User_id] = newUser(user)
			}
			tempData.Users[user.User_id].Caches[state] = append(tempData.Users[user.User_id].Caches[state], currentQ)
		}
	}

	for cacheType, _ := range tempData.Caches {
		sort.Sort(byCreationDate(tempData.Caches[cacheType]))
	}

	return tempData, time.Now().Unix(), nil
}

//Function called when the /viewTags request is made
//Retrieves all distinct tags and the number of questions saved in the db with that tag
func readTagsFromDb(ctx context.Context) []tagData {
	var tempData []tagData
	var (
		tag   sql.NullString
		count sql.NullInt64
	)

	rows, err := db.Query("SELECT tag, COUNT(tag) FROM question_tag GROUP BY tag")
	if err != nil {
		log.Warningf(ctx, "Tag query failed: %v", err.Error())
		return tempData
	}

	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&tag, &count)
		if err != nil {
			log.Warningf(ctx, "Tag Scan failed: %v", err.Error())
			continue
		}
		currentTag := tagData{tag.String, int(count.Int64)}
		tempData = append(tempData, currentTag)
	}

	return tempData
}

// Function to read all user data filtered by params from the database when a /viewUsers request is made
// Retrieves all users data
func readUsersFromDb(ctx context.Context, params string) map[int]userData {

	tempData := make(map[int]userData)

	var (
		id   sql.NullInt64
		name sql.NullString
		pic  sql.NullString
		link sql.NullString
	)

	query := "SELECT * FROM user"
	if params != "" {
		query += " WHERE " + params
	}
	rows, err := db.Query(query)
	if err != nil {
		if ctx != nil {
			log.Warningf(ctx, "User query failed: %v", err.Error())
		}
		return tempData
	}

	defer rows.Close()
	for rows.Next() {
		err := rows.Scan(&id, &name, &pic, &link)
		if err != nil {
			if ctx != nil {
				log.Warningf(ctx, "User Scan failed: %v", err.Error())
			}
			continue
		}

		currentUser := stackongo.User{
			User_id:       int(id.Int64),
			Display_name:  name.String,
			Profile_image: pic.String,
			Link:          link.String,
		}
		tempData[int(id.Int64)] = newUser(currentUser)
	}

	return tempData
}

/* Function to check if the DB has been updated since we last queried it
Returns true if our cache needs to be refreshed
False if is all g */
func checkDBUpdateTime(ctx context.Context, tableName string, lastUpdate int64) bool {
	var (
		last_updated int64
	)
	err := db.QueryRow("SELECT last_updated FROM update_times WHERE table_name='" + tableName + "'").Scan(&last_updated)
	if err != nil {
		log.Errorf(ctx, "Update time scan failed: %v", err.Error())
	}
	return last_updated > lastUpdate
}

func readUserFromDb(ctx context.Context, username string) stackongo.User {
	//Reading from database
	log.Infof(ctx, "Refreshing database read")
	var (
		id    sql.NullInt64
		name  sql.NullString
		image sql.NullString
	)
	//Select all questions in the database and read into a new data object
	err := db.QueryRow("SELECT id, name, pic FROM user WHERE name='"+username+"'").Scan(&id, &name, &image)
	if err != nil {
		log.Errorf(ctx, "User Scan failed: %v", err.Error())
		return stackongo.User{}
	}

	if id.Valid {
		return stackongo.User{
			User_id:       int(id.Int64),
			Display_name:  name.String,
			Profile_image: image.String,
		}
	}
	return stackongo.User{}
}

// Write user data into the database
func addUserToDB(ctx context.Context, newUser stackongo.User) {

	stmts, err := db.Prepare("INSERT IGNORE INTO user (id, name, pic) VALUES (?, ?, ?)")
	if err != nil {
		log.Infof(ctx, "Prepare failed: %v", err.Error())
		return
	}

	_, err = stmts.Exec(newUser.User_id, newUser.Display_name, newUser.Profile_image)
	if err != nil {
		log.Errorf(ctx, "Insertion of new user failed: %v", err.Error())
	}
}

// Updates the login time for the current user
func updateLoginTime(ctx context.Context, user stackongo.User) {
	stmts, err := db.Prepare("UPDATE user SET last_login=? WHERE id=?")
	if err != nil {
		log.Errorf(ctx, "Update login time failed: %v", err.Error())
	}

	time := time.Now().Unix()

	_, err = stmts.Exec(time, user.User_id)
	if err != nil {
		log.Errorf(ctx, "Execution of login update failed: %v", err.Error())
	}

	log.Infof(ctx, "Login time of user %s updated to %s!", user.User_id, time)
}

// updating the caches based on input from the app
// Returns time of update
func updatingCache_User(ctx context.Context, r *http.Request, user stackongo.User) (int64, error) {
	log.Infof(ctx, "updating cache")

	updateTime := time.Now().Unix()

	// required to collect post form data
	r.ParseForm()

	cache := r.PostFormValue("cache")
	qnID, _ := strconv.Atoi(r.PostFormValue("question_id"))
	form_input := r.PostFormValue("state")

	// Update the database
	if err := backend.UpdateQns(db, ctx, qnID, cache, form_input, user.User_id, mostRecentUpdate); err != nil {
		return int64(0), err
	}
	return updateTime, nil
}
