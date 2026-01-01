package attachment

import (
	"fmt"
	"time"
)

// ProcessedAttachment represents an attachment after processing and storage
type ProcessedAttachment struct {
	ID          string           `json:"id"`
	EmailID     string           `json:"email_id"`
	Filename    string           `json:"filename"`
	ContentType string           `json:"content_type"`
	SizeBytes   int64            `json:"size_bytes"`
	StorageKey  string           `json:"storage_key"`
	StorageURL  string           `json:"storage_url"`
	Checksum    string           `json:"checksum"` // SHA-256 checksum
	Status      AttachmentStatus `json:"status"`   // Requirements: 1.10 - Track attachment status
	CreatedAt   time.Time        `json:"created_at"`
}

// AttachmentValidationError represents an attachment validation error
type AttachmentValidationError struct {
	Filename string `json:"filename"`
	Reason   string `json:"reason"`
}

// Error implements the error interface
func (e *AttachmentValidationError) Error() string {
	return e.Reason
}

// Size limits per Requirements 5.6, 5.7
const (
	MaxAttachmentSize      = 10 * 1024 * 1024 // 10 MB per attachment
	MaxTotalAttachmentSize = 25 * 1024 * 1024 // 25 MB total per email
)

// DangerousExtensions lists blocked file extensions per Requirements 5.9
var DangerousExtensions = map[string]bool{
	".exe": true,
	".bat": true,
	".cmd": true,
	".vbs": true,
	".js":  true,
	".jar": true,
	".msi": true,
	".scr": true,
	".pif": true,
	".com": true,
}

// PathTraversalChars lists characters to sanitize from filenames per Requirements 5.8
var PathTraversalChars = []string{
	"..",
	"/",
	"\\",
	"\x00",
}

// ContentTypeExtensionMap maps content types to their valid file extensions
// Requirements: 2.2 - Validate content-type matches file extension
var ContentTypeExtensionMap = map[string][]string{
	// Documents
	"application/pdf":  {".pdf"},
	"application/msword": {".doc"},
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": {".docx"},
	"application/vnd.ms-excel": {".xls"},
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": {".xlsx"},
	"application/vnd.ms-powerpoint": {".ppt"},
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": {".pptx"},
	"text/plain":       {".txt", ".text", ".log", ".md", ".csv"},
	"text/csv":         {".csv"},
	"text/html":        {".html", ".htm"},
	"text/xml":         {".xml"},
	"application/xml":  {".xml"},
	"application/json": {".json"},
	
	// Images
	"image/jpeg":    {".jpg", ".jpeg"},
	"image/png":     {".png"},
	"image/gif":     {".gif"},
	"image/webp":    {".webp"},
	"image/svg+xml": {".svg"},
	"image/bmp":     {".bmp"},
	"image/tiff":    {".tiff", ".tif"},
	"image/x-icon":  {".ico"},
	
	// Archives
	"application/zip":              {".zip"},
	"application/x-rar-compressed": {".rar"},
	"application/x-7z-compressed":  {".7z"},
	"application/x-tar":            {".tar"},
	"application/gzip":             {".gz", ".gzip"},
	"application/x-bzip2":          {".bz2"},
	
	// Audio
	"audio/mpeg":  {".mp3"},
	"audio/wav":   {".wav"},
	"audio/ogg":   {".ogg"},
	"audio/flac":  {".flac"},
	"audio/aac":   {".aac"},
	
	// Video
	"video/mp4":       {".mp4"},
	"video/mpeg":      {".mpeg", ".mpg"},
	"video/quicktime": {".mov"},
	"video/x-msvideo": {".avi"},
	"video/webm":      {".webm"},
	
	// Generic binary
	"application/octet-stream": {}, // Accepts any extension
}

// ExtensionContentTypeMap maps file extensions to their expected content types
// Requirements: 2.2 - Validate content-type matches file extension
var ExtensionContentTypeMap = map[string][]string{
	// Documents
	".pdf":  {"application/pdf"},
	".doc":  {"application/msword"},
	".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	".xls":  {"application/vnd.ms-excel"},
	".xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	".ppt":  {"application/vnd.ms-powerpoint"},
	".pptx": {"application/vnd.openxmlformats-officedocument.presentationml.presentation"},
	".txt":  {"text/plain"},
	".text": {"text/plain"},
	".log":  {"text/plain"},
	".md":   {"text/plain", "text/markdown"},
	".csv":  {"text/csv", "text/plain"},
	".html": {"text/html"},
	".htm":  {"text/html"},
	".xml":  {"text/xml", "application/xml"},
	".json": {"application/json", "text/json"},
	
	// Images
	".jpg":  {"image/jpeg"},
	".jpeg": {"image/jpeg"},
	".png":  {"image/png"},
	".gif":  {"image/gif"},
	".webp": {"image/webp"},
	".svg":  {"image/svg+xml"},
	".bmp":  {"image/bmp"},
	".tiff": {"image/tiff"},
	".tif":  {"image/tiff"},
	".ico":  {"image/x-icon", "image/vnd.microsoft.icon"},
	
	// Archives
	".zip":  {"application/zip", "application/x-zip-compressed"},
	".rar":  {"application/x-rar-compressed", "application/vnd.rar"},
	".7z":   {"application/x-7z-compressed"},
	".tar":  {"application/x-tar"},
	".gz":   {"application/gzip", "application/x-gzip"},
	".gzip": {"application/gzip"},
	".bz2":  {"application/x-bzip2"},
	
	// Audio
	".mp3":  {"audio/mpeg"},
	".wav":  {"audio/wav", "audio/x-wav"},
	".ogg":  {"audio/ogg"},
	".flac": {"audio/flac"},
	".aac":  {"audio/aac"},
	
	// Video
	".mp4":  {"video/mp4"},
	".mpeg": {"video/mpeg"},
	".mpg":  {"video/mpeg"},
	".mov":  {"video/quicktime"},
	".avi":  {"video/x-msvideo"},
	".webm": {"video/webm"},
}

// ContentTypeMismatchError represents a content-type validation error
type ContentTypeMismatchError struct {
	Filename        string
	DeclaredType    string
	ExpectedTypes   []string
}

// Error implements the error interface
func (e *ContentTypeMismatchError) Error() string {
	return fmt.Sprintf("content-type mismatch for %s: declared %s, expected one of %v", e.Filename, e.DeclaredType, e.ExpectedTypes)
}

// ExecutableMagicBytes defines magic byte signatures for executable files
// Requirements: 2.3 - Scan file magic bytes to detect disguised executables
type MagicSignature struct {
	Name        string
	Signature   []byte
	Offset      int
	Description string
}

// ExecutableMagicSignatures lists magic byte signatures for executable files
// Requirements: 2.3 - Detect disguised executables
var ExecutableMagicSignatures = []MagicSignature{
	// Windows PE executable (MZ header)
	{
		Name:        "Windows PE",
		Signature:   []byte{0x4D, 0x5A}, // "MZ"
		Offset:      0,
		Description: "Windows PE executable",
	},
	// Linux ELF executable
	{
		Name:        "Linux ELF",
		Signature:   []byte{0x7F, 0x45, 0x4C, 0x46}, // "\x7FELF"
		Offset:      0,
		Description: "Linux ELF executable",
	},
	// Mach-O 32-bit executable (macOS)
	{
		Name:        "Mach-O 32-bit",
		Signature:   []byte{0xFE, 0xED, 0xFA, 0xCE},
		Offset:      0,
		Description: "Mach-O 32-bit executable",
	},
	// Mach-O 64-bit executable (macOS)
	{
		Name:        "Mach-O 64-bit",
		Signature:   []byte{0xFE, 0xED, 0xFA, 0xCF},
		Offset:      0,
		Description: "Mach-O 64-bit executable",
	},
	// Mach-O 32-bit reverse byte order
	{
		Name:        "Mach-O 32-bit (reverse)",
		Signature:   []byte{0xCE, 0xFA, 0xED, 0xFE},
		Offset:      0,
		Description: "Mach-O 32-bit executable (reverse byte order)",
	},
	// Mach-O 64-bit reverse byte order
	{
		Name:        "Mach-O 64-bit (reverse)",
		Signature:   []byte{0xCF, 0xFA, 0xED, 0xFE},
		Offset:      0,
		Description: "Mach-O 64-bit executable (reverse byte order)",
	},
	// Java class file
	{
		Name:        "Java Class",
		Signature:   []byte{0xCA, 0xFE, 0xBA, 0xBE},
		Offset:      0,
		Description: "Java class file",
	},
	// MS-DOS executable (COM file)
	{
		Name:        "MS-DOS COM",
		Signature:   []byte{0xE9}, // JMP instruction
		Offset:      0,
		Description: "MS-DOS COM executable",
	},
	// Windows Script Host (WSH)
	{
		Name:        "Windows Script",
		Signature:   []byte{0x3C, 0x6A, 0x6F, 0x62}, // "<job"
		Offset:      0,
		Description: "Windows Script Host file",
	},
}

// DisguisedExecutableError represents a disguised executable detection error
type DisguisedExecutableError struct {
	Filename      string
	DetectedType  string
	Description   string
}

// Error implements the error interface
func (e *DisguisedExecutableError) Error() string {
	return fmt.Sprintf("disguised executable detected in %s: %s (%s)", e.Filename, e.DetectedType, e.Description)
}

// AttachmentStatus represents the status of an attachment upload
// Requirements: 1.10 - Track attachment status for failed uploads
type AttachmentStatus string

const (
	// AttachmentStatusActive indicates the attachment was successfully uploaded
	AttachmentStatusActive AttachmentStatus = "active"
	// AttachmentStatusFailed indicates the attachment upload permanently failed
	AttachmentStatusFailed AttachmentStatus = "failed"
	// AttachmentStatusPending indicates the attachment upload is in progress
	AttachmentStatusPending AttachmentStatus = "pending"
)

// Retry configuration constants
// Requirements: 1.9 - Retry up to 3 times with exponential backoff
const (
	// MaxUploadRetries is the maximum number of retry attempts for uploads
	MaxUploadRetries = 3
	// InitialRetryDelay is the initial delay before first retry
	InitialRetryDelay = 100 // milliseconds
	// MaxRetryDelay is the maximum delay between retries
	MaxRetryDelay = 2000 // milliseconds
	// RetryBackoffMultiplier is the multiplier for exponential backoff
	RetryBackoffMultiplier = 2
)

// UploadError represents an upload error with retry information
// Requirements: 1.9, 1.10 - Track upload failures and retry attempts
type UploadError struct {
	Filename     string
	Attempts     int
	LastError    error
	IsPermanent  bool
}

// Error implements the error interface
func (e *UploadError) Error() string {
	if e.IsPermanent {
		return fmt.Sprintf("permanent upload failure for %s after %d attempts: %v", e.Filename, e.Attempts, e.LastError)
	}
	return fmt.Sprintf("upload failed for %s (attempt %d): %v", e.Filename, e.Attempts, e.LastError)
}

// FailedAttachment represents an attachment that failed to upload
// Requirements: 1.10 - Mark attachment as failed on permanent failure
type FailedAttachment struct {
	Filename    string
	ContentType string
	SizeBytes   int64
	Reason      string
	Attempts    int
	LastError   string
}
