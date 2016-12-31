/*
 * Minio Cloud Storage, (C) 2015, 2016 Minio, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"strings"
)

// Validates location constraint in PutBucket request body.
// The location value in the request body should match the
// region configured at serverConfig, otherwise error is returned.
func isValidLocationConstraint(r *http.Request) (s3Error APIErrorCode) {
	console.Println("Running is Valid")
	
	serverRegion := serverConfig.GetRegion()
	// If the request has no body with content-length set to 0,
	// we do not have to validate location constraint. Bucket will
	// be created at default region.
	locationConstraint := createBucketLocationConfiguration{}
	err := xmlDecoder(r.Body, &locationConstraint, r.ContentLength)
	console.Println("after decoder");
	if err == nil || err == io.EOF {
		// Successfully decoded, proceed to verify the region.
		// Once region has been obtained we proceed to verify it.
		incomingRegion := locationConstraint.Location
		if incomingRegion == "" {
			// Location constraint is empty for region "us-east-1",
			// in accordance with protocol.
			incomingRegion = "us-east-1"
		}
		// Return errInvalidRegion if location constraint does not match
		// with configured region.
		s3Error = ErrNone
		console.Println("these are the regions noe server")
		console.Println(serverRegion)
		console.Println("now incoming")
		console.Println(incomingRegion)
		
		if serverRegion != incomingRegion {
			s3Error = ErrInvalidRegion
		}
		return s3Error
	}
	errorIf(err, "Unable to xml decode location constraint")
	// Treat all other failures as XML parsing errors.
	console.Println("We are at the bottom")
	
	return ErrMalformedXML
}

// Supported headers that needs to be extracted.
var supportedHeaders = []string{
	"content-type",
	"cache-control",
	"content-encoding",
	"content-disposition",
	// Add more supported headers here.
}

// isMetadataDirectiveValid - check if metadata-directive is valid.
func isMetadataDirectiveValid(h http.Header) bool {
	_, ok := h[http.CanonicalHeaderKey("X-Amz-Metadata-Directive")]
	if ok {
		// Check atleast set metadata-directive is valid.
		return (isMetadataCopy(h) || isMetadataReplace(h))
	}
	// By default if x-amz-metadata-directive is not we
	// treat it as 'COPY' this function returns true.
	return true
}

// Check if the metadata COPY is requested.
func isMetadataCopy(h http.Header) bool {
	return h.Get("X-Amz-Metadata-Directive") == "COPY"
}

// Check if the metadata REPLACE is requested.
func isMetadataReplace(h http.Header) bool {
	return h.Get("X-Amz-Metadata-Directive") == "REPLACE"
}

// Splits an incoming path into bucket and object components.
func path2BucketAndObject(path string) (bucket, object string) {
	// Skip the first element if it is '/', split the rest.
	path = strings.TrimPrefix(path, "/")
	pathComponents := strings.SplitN(path, "/", 2)

	// Save the bucket and object extracted from path.
	switch len(pathComponents) {
	case 1:
		bucket = pathComponents[0]
	case 2:
		bucket = pathComponents[0]
		object = pathComponents[1]
	}
	return bucket, object
}

// extractMetadataFromHeader extracts metadata from HTTP header.
func extractMetadataFromHeader(header http.Header) map[string]string {
	metadata := make(map[string]string)
	// Save standard supported headers.
	for _, supportedHeader := range supportedHeaders {
		canonicalHeader := http.CanonicalHeaderKey(supportedHeader)
		// HTTP headers are case insensitive, look for both canonical
		// and non canonical entries.
		if _, ok := header[canonicalHeader]; ok {
			metadata[supportedHeader] = header.Get(canonicalHeader)
		} else if _, ok := header[supportedHeader]; ok {
			metadata[supportedHeader] = header.Get(supportedHeader)
		}
	}
	// Go through all other headers for any additional headers that needs to be saved.
	for key := range header {
		cKey := http.CanonicalHeaderKey(key)
		if strings.HasPrefix(cKey, "X-Amz-Meta-") {
			metadata[cKey] = header.Get(key)
		} else if strings.HasPrefix(key, "X-Minio-Meta-") {
			metadata[cKey] = header.Get(key)
		}
	}
	// Return.
	return metadata
}

// extractMetadataFromForm extracts metadata from Post Form.
func extractMetadataFromForm(formValues map[string]string) map[string]string {
	metadata := make(map[string]string)
	// Save standard supported headers.
	for _, supportedHeader := range supportedHeaders {
		canonicalHeader := http.CanonicalHeaderKey(supportedHeader)
		// Form field names are case insensitive, look for both canonical
		// and non canonical entries.
		if _, ok := formValues[canonicalHeader]; ok {
			metadata[supportedHeader] = formValues[canonicalHeader]
		} else if _, ok := formValues[supportedHeader]; ok {
			metadata[supportedHeader] = formValues[canonicalHeader]
		}
	}
	// Go through all other form values for any additional headers that needs to be saved.
	for key := range formValues {
		cKey := http.CanonicalHeaderKey(key)
		if strings.HasPrefix(cKey, "X-Amz-Meta-") {
			metadata[cKey] = formValues[key]
		} else if strings.HasPrefix(cKey, "X-Minio-Meta-") {
			metadata[cKey] = formValues[key]
		}
	}
	return metadata
}

// Extract form fields and file data from a HTTP POST Policy
func extractPostPolicyFormValues(reader *multipart.Reader) (filePart io.Reader, fileName string, formValues map[string]string, err error) {
	/// HTML Form values
	formValues = make(map[string]string)
	fileName = ""
	for err == nil {
		var part *multipart.Part
		part, err = reader.NextPart()
		if part != nil {
			canonicalFormName := http.CanonicalHeaderKey(part.FormName())
			if canonicalFormName != "File" {
				var buffer []byte
				limitReader := io.LimitReader(part, maxFormFieldSize+1)
				buffer, err = ioutil.ReadAll(limitReader)
				if err != nil {
					return nil, "", nil, err
				}
				if int64(len(buffer)) > maxFormFieldSize {
					return nil, "", nil, errSizeUnexpected
				}
				formValues[canonicalFormName] = string(buffer)
			} else {
				filePart = part
				fileName = part.FileName()
				// As described in S3 spec, we expect file to be the last form field
				break
			}
		}
	}
	return filePart, fileName, formValues, nil
}
