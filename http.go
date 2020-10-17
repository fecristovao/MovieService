package main

import (
	"encoding/json"
	"fmt"
	m "github.com/log22/MoviesCrawler"
    "reflect"
    "net/http"
    "log"
    "github.com/gorilla/mux"
    "sync"
    "os"
    "path/filepath"
    "os/exec"
	"regexp"
	"strings"
    "errors"
    "time"
)

// Request struct for Search POST
type SearchRequest struct {
    Services []string
    MovieName string
}

// Request struct for get downloads links POST
type DownloadLinksRequest struct {
    Service string
    Movie m.Movie
}

// Response struct for Search Request
type FoundMovies struct {
    Service string
    Movies m.FoundMovies
}

type RequestAddDownload struct {
    Link string
}

var allServices map[string]m.Crawler
var torrentIP = "192.168.0.108"

// spaHandler implements the http.Handler interface, so we can use it
// to respond to HTTP requests. The path to the static directory and
// path to the index file within that static directory are used to
// serve the SPA in the given static directory.
type spaHandler struct {
	staticPath string
	indexPath  string
}

// ServeHTTP inspects the URL path to locate a file within the static dir
// on the SPA handler. If a file is found, it will be served. If not, the
// file located at the index path on the SPA handler will be served. This
// is suitable behavior for serving an SPA (single page application).
func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // get the absolute path to prevent directory traversal
	path, err := filepath.Abs(r.URL.Path)
	if err != nil {
        // if we failed to get the absolute path respond with a 400 bad request
        // and stop
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

    // prepend the path with the path to the static directory
	path = filepath.Join(h.staticPath, path)

    // check whether a file exists at the given path
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		// file does not exist, serve index.html
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	} else if err != nil {
        // if we got an error (that wasn't that the file doesn't exist) stating the
        // file, return a 500 internal server error and stop
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

    // otherwise, use http.FileServer to serve the static dir
	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

type Torrent struct {
	ID string
	Done, Downloaded, ETA, Up, Down, Ratio, Status, Name string
}

func ListTorrents(ip string) (error, []Torrent) {
    out, err := exec.Command("/usr/bin/transmission-remote", ip,"-l").Output()
	if err != nil {
		log.Fatal(err)
		return errors.New("Error"), nil
	}
	
	// Split Lines into array
	lines := strings.Split(string(out), "\n")
	
	// Remove first and last line
	lines = lines[1:len(lines)-2]

	// Remove all 2 spaces
	re := regexp.MustCompile(`\s{2,}`)

	var torrents []Torrent
	for _, line := range lines {
        items := strings.Split(re.ReplaceAllString(line, "\t"), "\t")[1:]
        id := strings.ReplaceAll(items[0], "*", "")
		torrent := Torrent{id, items[1], items[2], items[3], items[4], items[5], items[6], items[7], strings.Join(items[8:],"")}
		torrents = append(torrents, torrent)
	}

	return nil, torrents
}

func AddTorrent(ip string, magnetLink string) bool {
	out, err := exec.Command("/usr/bin/transmission-remote", ip,"-a", magnetLink).Output()
	if err != nil {
		log.Fatal(err)
		return false
	}
	
	if strings.Contains(string(out), "Error") {
		return false
	} else {
		return true
	}
		
}

func StopTorrent(ip string, ID string) bool {
	out, err := exec.Command("/usr/bin/transmission-remote", ip,"-t", ID, "-S").Output()
	if err != nil {
		log.Fatal(err)
		return false
	}
	if strings.Contains(string(out), "Error") {
		return false
	} else {
		return true
	}
}

func ResumeTorrent(ip string, ID string) bool {
	out, err := exec.Command("/usr/bin/transmission-remote", ip,"-t", ID, "-s").Output()
	if err != nil {
		log.Fatal(err)
		return false
	}
	if strings.Contains(string(out), "Error") {
		return false
	} else {
		return true
	}
}

func DeleteTorrent(ip string, ID string) bool {
	out, err := exec.Command("/usr/bin/transmission-remote", ip,"-t", ID, "-rad").Output()
	if err != nil {
		log.Fatal(err)
		return false
	}
	if strings.Contains(string(out), "Error") {
		return false
	} else {
		return true
	}
}

func getType(myvar interface{}) string {
	if t := reflect.TypeOf(myvar); t.Kind() == reflect.Ptr {
		return "*" + t.Elem().Name()
	} else {
		return t.Name()
	}
}

func setServices() map[string]m.Crawler {
    services := make(map[string]m.Crawler)
    services["MegaTorrents"] = m.MegaTorrents{}
    services["BludTV"] = m.BludTV{}
    services["ComandoTorrents"] = m.ComandoTorrents{}
    services["MeusFilmesTorrent"] = m.MeusFilmesTorrent{}
    services["PirateTorrent"] = m.PirateTorrent{}
    return services
}

func listServices(w http.ResponseWriter, r *http.Request) {
    // Set Header for Options
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Headers", "*")

    fmt.Printf("[%s] - Requested listServices\n", time.Now().Format("2006-01-02 15:04:05"))
    var listServices []string
    for key, _ := range allServices {
        listServices = append(listServices, key)
    }
    json.NewEncoder(w).Encode(listServices)   
} 

func searchMovie(w http.ResponseWriter, r *http.Request) {
    // Set Header for Options
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Headers", "*")
    
    // If is a option method, terminate function
    if r.Method == http.MethodOptions {
        return
    }
    
    // Decode JSON request into structure
    var postParams SearchRequest
    err := json.NewDecoder(r.Body).Decode(&postParams)

    // If throw an error when JSON decode
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    fmt.Printf("[%s] - Requested searchMovie: %+v\n", time.Now().Format("2006-01-02 15:04:05"), postParams)

    var wg sync.WaitGroup
    var mutex sync.Mutex
    var results []FoundMovies

    // For each service requested, start one goroutine
    for _, service := range postParams.Services {
        fmt.Printf("Searching %s\n", service)
        
        // Add one go routine to group counter
        wg.Add(1)

        // Launch goroutine
        go func(service m.Crawler, wg *sync.WaitGroup, mutex *sync.Mutex, total *[]FoundMovies, postParams SearchRequest) {
            defer wg.Done()

            serviceStr := getType(service)

            // Search movie
            movieLinks := m.SearchAll(service, postParams.MovieName)
            fmt.Printf("Found %d on %s\n", len(movieLinks), serviceStr)
            
            if movieLinks != nil {
                mutex.Lock()
                results = append(results, FoundMovies{serviceStr, movieLinks})
                mutex.Unlock()
            }

        }(allServices[service], &wg, &mutex, &results, postParams)
    }

    // Wait all services finish
    wg.Wait()

    // Send found movies
    json.NewEncoder(w).Encode(results) 
}

func getMagnetLinks(w http.ResponseWriter, r *http.Request) {
    // Set Header for Options
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Headers", "*")

    // If is a option method, terminate function
    if r.Method == http.MethodOptions {
        return
    }
    
    // Receive a movie as params
    var postParams DownloadLinksRequest
    err := json.NewDecoder(r.Body).Decode(&postParams)

    // If throw an error when JSON decode
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    fmt.Printf("[%s] - Requested getMagnetLinks: %+v\n", time.Now().Format("2006-01-02 15:04:05"), postParams)   
    // Get service struct
    service := allServices[postParams.Service]

    // Get Download Link
    downloadLink := postParams.Movie.Link

    // Get MagnetLinks
    var resultOptions m.FoundMagnetLinks
    resultOptions = service.GetDownloadLinks(downloadLink)
    
    // Send found links
    json.NewEncoder(w).Encode(resultOptions)
}

func listAllTorrents(w http.ResponseWriter, r *http.Request) {
    // Set Header for Options
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Headers", "*")

    fmt.Printf("[%s] - Requested listAllTorrents\n", time.Now().Format("2006-01-02 15:04:05"))
    err, list := ListTorrents(torrentIP)
    if err != nil {
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    json.NewEncoder(w).Encode(list)
} 

func addMagnetLink(w http.ResponseWriter, r *http.Request) {
    // Set Header for Options
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Headers", "*")
    
    // If is a option method, terminate function
    if r.Method == http.MethodOptions {
        return
    }

    // Decode JSON request into structure
    var postParams RequestAddDownload
    err := json.NewDecoder(r.Body).Decode(&postParams)

    // If throw an error when JSON decode
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    fmt.Printf("[%s] - Requested addMagnetLink: %+v\n", time.Now().Format("2006-01-02 15:04:05"), postParams)
    result := AddTorrent(torrentIP, postParams.Link)

    if !result {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    json.NewEncoder(w).Encode(result)

}

func deleteTorrent(w http.ResponseWriter, r *http.Request) {
    // Set Header for Options
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Headers", "*")

    params := mux.Vars(r)
    id := params["id"]
    result := DeleteTorrent(torrentIP, id)
    
    if !result {
        w.WriteHeader(http.StatusBadRequest)
        return
    }
    fmt.Printf("[%s] - Requested deleteTorrent: %s\n", time.Now().Format("2006-01-02 15:04:05"), id)
    json.NewEncoder(w).Encode(result)
}

func resumeTorrent(w http.ResponseWriter, r *http.Request) {
    // Set Header for Options
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Headers", "*")

    params := mux.Vars(r)
    id := params["id"]
    result := ResumeTorrent(torrentIP, id)
    
    if !result {
        w.WriteHeader(http.StatusBadRequest)
        return
    }
    fmt.Printf("[%s] - Requested resumeTorrent: %s\n", time.Now().Format("2006-01-02 15:04:05"), id)
    json.NewEncoder(w).Encode(result)
}

func pauseTorrent(w http.ResponseWriter, r *http.Request) {
    // Set Header for Options
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Headers", "*")
    

    params := mux.Vars(r)
    id := params["id"]
    result := StopTorrent(torrentIP, id)
    
    if !result {
        w.WriteHeader(http.StatusBadRequest)
        return
    }
    fmt.Printf("[%s] - Requested pauseTorrent: %s\n", time.Now().Format("2006-01-02 15:04:05"), id)
    json.NewEncoder(w).Encode(result)
}

func main() {
    ip, _ := exec.Command("hostname", "-I").Output()
    ipF := strings.ReplaceAll(string(ip), "\n", "")
    ipF = strings.ReplaceAll(ipF, "\t", "")
    ips := strings.Split(ipF, " ")
    torrentIP = ips[0]

    fmt.Printf("IP: %s\n", torrentIP)
    allServices = setServices()
    r := mux.NewRouter()
    
    // Handle API routes
    api := r.PathPrefix("/api/").Subrouter()
    api.HandleFunc("/listservices", listServices)
    api.HandleFunc("/searchMovie", searchMovie).Methods("POST", "OPTIONS")
    api.HandleFunc("/getMagnetLinks", getMagnetLinks).Methods("POST", "OPTIONS")

    // Torrent Endpoints
    api.HandleFunc("/listTorrents", listAllTorrents).Methods("GET")
    api.HandleFunc("/addMagnetLink", addMagnetLink).Methods("POST", "OPTIONS")
    api.HandleFunc("/deleteTorrent/{id}", deleteTorrent).Methods("GET")
    api.HandleFunc("/resumeTorrent/{id}", resumeTorrent).Methods("GET")
    api.HandleFunc("/pauseTorrent/{id}", pauseTorrent).Methods("GET")

    // SPA route
    spa := spaHandler{staticPath: "./spa", indexPath: "index.html"}
    r.PathPrefix("/").Handler(spa)
    
    // Set CORS
    r.Use(mux.CORSMethodMiddleware(r))

    // Listen
    fmt.Printf("[%s] - Starting MoviesPI Service\n", time.Now().Format("2006-01-02 15:04:05"))
    log.Fatal(http.ListenAndServe(":8888", r))
}
