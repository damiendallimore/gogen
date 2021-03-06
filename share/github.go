package share

// Mostly from https://jacobmartins.com/2016/02/29/getting-started-with-oauth2-in-go/

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/kr/pretty"
	uuid "github.com/satori/go.uuid"
	"github.com/skratchdot/open-golang/open"

	config "github.com/coccyx/gogen/internal"
	log "github.com/coccyx/gogen/logger"
	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
	githuboauth "golang.org/x/oauth2/github"
)

var (
	gitHubClientID     string // Passed in during the build process
	gitHubClientSecret string // Passed in during the build process
	oauthConf          = &oauth2.Config{
		RedirectURL:  "http://localhost:46436/GitHubCallback",
		ClientID:     gitHubClientID,
		ClientSecret: gitHubClientSecret,
		Endpoint:     githuboauth.Endpoint,
		Scopes:       []string{"gist"},
	}
	// Some random string, random for each request
	oauthStateString = uuid.NewV4().String()
)

// GitHub allows posting gists to a user's GitHub
type GitHub struct {
	done   chan int
	token  string
	client *github.Client
	c      *config.Config
}

// Push will create a public gist of "name.json" from our running config
func (gh *GitHub) Push(name string) *github.Gist {
	gist := new(github.Gist)
	files := make(map[github.GistFilename]github.GistFile)

	file := new(github.GistFile)
	fname := name + ".json"
	file.Filename = &fname
	var outb []byte
	var outbs *string
	var err error
	if outb, err = json.MarshalIndent(gh.c, "", "  "); err != nil {
		log.Fatalf("Cannot Marshal c.Global, err: %s", err)
	}
	outbs = new(string)
	*outbs = string(outb)
	file.Content = outbs
	files[github.GistFilename(name)] = *file

	gist.Files = files
	gist.Description = &name
	public := true
	gist.Public = &public

	var foundgist *github.Gist
	foundgist = gh.findgist(name)

	if foundgist != nil {
		log.Debugf("Found gist, updating")
		updatedgist, _, err := gh.client.Gists.Edit(*foundgist.ID, gist)
		if err != nil {
			log.Fatalf("Error updating gist %# v: %s", pretty.Formatter(gist), err)
		}
		return updatedgist
	}
	log.Debugf("Gist not found, creating")
	newgist, _, err := gh.client.Gists.Create(gist)
	if err != nil {
		log.Fatalf("Error creating gist %# v: %s", pretty.Formatter(gist), err)
	}
	return newgist
}

// Pull grabs a named gist from GitHub
func (gh *GitHub) Pull(name string) *github.Gist {
	var gist *github.Gist
	gist = gh.findgist(name)
	if gist == nil {
		log.Fatalf("Error finding gist %s", name+".json")
	}
	return gist
}

func (gh *GitHub) findgist(name string) (foundgist *github.Gist) {
	gists, _, err := gh.client.Gists.List("", nil)
	if err != nil {
		log.Fatalf("Cannot pull Gists list: %s", err)
	}
findgist:
	for _, testgist := range gists {
		for _, gistfile := range testgist.Files {
			log.Debugf("Testing %s if match %s", *gistfile.Filename, name+".json")
			if *gistfile.Filename == name+".json" {
				foundgist = testgist
				break findgist
			}
		}
	}
	return foundgist
}

// NewGitHub returns a GitHub object, with a set auth token
func NewGitHub(requireauth bool) *GitHub {
	gh := new(GitHub)
	gh.done = make(chan int)

	if oauthConf.ClientID == "" {
		oauthConf.ClientID = os.Getenv("GITHUB_OAUTH_CLIENT_ID")
	}
	if oauthConf.ClientSecret == "" {
		oauthConf.ClientSecret = os.Getenv("GITHUB_OAUTH_CLIENT_SECRET")
	}

	c := config.NewConfig()
	gh.c = c
	tokenFile := os.ExpandEnv("$GOGEN_HOME/.githubtoken")
	_, err := os.Stat(tokenFile)
	if err == nil {
		buf, err := ioutil.ReadFile(tokenFile)
		if err != nil {
			log.Fatalf("Error reading from file %s: %s", tokenFile, err)
		}
		gh.token = string(buf)
		log.Debugf("Getting GitHub token '%s' from file", gh.token)
	} else if requireauth {
		if !os.IsNotExist(err) {
			log.Fatalf("Unexpected error accessing %s: %s", tokenFile, err)
		}
		http.HandleFunc("/GitHubLogin", gh.handleGitHubLogin)
		open.Run("http://localhost:46436/GitHubLogin")
		http.HandleFunc("/GitHubCallback", gh.handleGitHubCallback)
		go http.ListenAndServe(":46436", nil)
		<-gh.done
		log.Debugf("Getting GitHub token '%s' from oauth", gh.token)

		err = ioutil.WriteFile(tokenFile, []byte(gh.token), 400)
		if err != nil {
			log.Fatalf("Error writing token to file %s: %s", tokenFile, err)
		}
	}
	if len(gh.token) > 0 {
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: gh.token},
		)
		tc := oauth2.NewClient(oauth2.NoContext, ts)
		gh.client = github.NewClient(tc)
	} else {
		gh.client = github.NewClient(nil)
	}
	return gh
}

func (gh *GitHub) handleGitHubLogin(w http.ResponseWriter, r *http.Request) {
	url := oauthConf.AuthCodeURL(oauthStateString)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (gh *GitHub) handleGitHubCallback(w http.ResponseWriter, r *http.Request) {
	state := r.FormValue("state")
	if state != oauthStateString {
		log.Errorf("invalid oauth state, expected '%s', got '%s'\n", oauthStateString, state)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	code := r.FormValue("code")
	token, err := oauthConf.Exchange(oauth2.NoContext, code)
	if err != nil {
		log.Errorf("Code exchange failed with '%s'\n", err)
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	gh.token = token.AccessToken
	gh.done <- 1
}
