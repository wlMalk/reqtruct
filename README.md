# Reqtruct
[![](https://godoc.org/github.com/wlMalk/reqtruct?status.svg)](http://godoc.org/github.com/wlMalk/reqtruct)

Reqtruct is a Go package to simplify the extraction and conversion of parameters from a http.Request

It borrows some code from [gorilla/schema](https://github.com/gorilla/schema).

It discovers your parameters based on the definitions you provide in struct tags and looks for them in headers, query params, path params, form, files or JSON.

If given no specification for where to look for params it will use the provided default location, if none is provided then it falls back to JSON.


# Example
First we define the structs to hold the data.
```go
type Image struct {
	ID      uuid.UUID             // type uuid.UUID implements TextUnmarshaller, location will be form
	Image   *multipart.FileHeader // uploaded file will be here
	Caption string                // location will form because of custom defaults
}

// AddImagesRequest is the struct that will hold the data for a AddImages request
type AddImagesRequest struct {
	Images        []*Image `name:"imgs"`   // provide custom alias name
	UserID        int      `from:"path"`   // decoder will look for UserID in path params
	Authorization string   `from:"header"` // decoder will get Authorization header
}

type Pagination struct {
	Limit  int
	Offset int
	Since  *Time
}

// GetImagesRequest is the struct that will hold the data for a GetImages request
type GetImagesRequest struct {
	// embedded structs pass their locations to their fields
	// only if no location is defined for the fields
	Pagination    `from:"query"`
	UserID        int    `from:"path"`
	Authorization string `from:"header,query"` // location will be either header or query
}

// Time is added to show how you can define a custom type for params
type Time struct {
	time.Time
}

const ctLayout = "2006/01/02|15:04:05"

func (t *Time) UnmarshalText(b []byte) (err error) {
	s := strings.Trim(string(b), "\"")
	if s == "" {
		t.Time = time.Time{}
		return
	}
	t.Time, err = time.Parse(ctLayout, s)
	return
}

func (t *Time) String() string {
	return t.Format(ctLayout)
}
```
Then in the main function we set up the decoder
```go
d := reqtruct.NewDecoder()
d.Separator('[', ']', 0) // paths will be in the format a[b][c][0][]...
d.NameFunc(func(name string, _ []int) string {
	// special function to convert a field name to its alias
	// only when no alias is provided
	// in this case it converts to snake case
	return ToSnakeCase(name)
})
d.DefaultLocation(reqtruct.LocationForm)
d.PathExtractor(func(r *http.Request) map[string]string {
	// function to extract path params from the router
	// in this case it is for julienschmidt/httprouter
	m := map[string]string{}
	params := httprouter.ParamsFromContext(r.Context())
	for _, p := range params {
		m[p.Key] = p.Value
	}
	return m
})
```
And finally we set up the router and use the decoder in the handlers
```go
r := httprouter.New()

r.Handler("POST", "/users/:user_id/images", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	req := &AddImagesRequest{}
	err := d.Decode(req, r)
	if err != nil {
		fmt.Fprintln(w, err)
		return
	}
	for _, img := range req.Images {
		if img.Image == nil {
			continue
		}
		fmt.Fprintln(w, req.UserID, req.Authorization, img.ID, img.Image.Filename, img.Caption)
	}
}))

r.Handler("GET", "/users/:user_id/images", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	req := &GetImagesRequest{}
	err := d.Decode(req, r)
	if err != nil {
		fmt.Fprintln(w, err)
		return
	}

	fmt.Fprintln(w, req.UserID, req.Authorization, req.Limit, req.Offset, req.Since)
}))

http.ListenAndServe(":8080", r)
```
Call `GET http://localhost:8080/users/1/images?offset=100&limit=10&since=2006/01/02|15:04:05&authorization=token`

Or call `POST http://localhost:8080/users/1/images` with the following params in the body as a `multipart/form-data`:
```
imgs[0][id]=5c33d4c9-a943-4a0b-a55b-82365daca7de
imgs[0][image]=@<first_file_path>
imgs[0][caption]=caption one
imgs[1][id]=56239022-f25b-4b35-9c1b-346c5d4767f9
imgs[1][image]=@<second_file_path>
imgs[1][caption]=caption two
```