package btree

import (
	"bytes"
	"encoding/binary"
	"errors"
)

const (
	PageSize = 4096 // Standard 4KB page size (matches OS page size)

	// Page types
	PageTypeInternal = 1
	PageTypeLeaf     = 2

	// Page format versions
	PageFormatV1 = 1 // Fixed 2-byte size encoding (legacy)
	PageFormatV2 = 2 // Variable-length size encoding (current)

	// Header offsets and sizes
	// Layout: [type(1)][numCells(2)][rightPtr(4)][freePtr(2)][version(1)] = 10 bytes total
	HeaderSize           = 10
	HeaderOffsetType     = 0
	HeaderOffsetNumCells = 1
	HeaderOffsetRightPtr = 3
	HeaderOffsetFreePtr  = 7
	HeaderOffsetVersion  = 9

	// Cell directory: 2 bytes per cell (offset from page start)
	CellDirEntrySize = 2

	// Cell header sizes (V1 - fixed encoding)
	LeafCellHeaderSizeV1     = 4 // key_size(2) + value_size(2)
	InternalCellHeaderSizeV1 = 6 // key_size(2) + child_page_id(4)

	// Cell header sizes (V2 - variable encoding)
	// These are minimums; actual size depends on key/value lengths
	LeafCellHeaderSizeV2Min     = 2 // key_size(varint) + value_size(varint) - minimum 1 byte each
	InternalCellHeaderSizeV2Min = 5 // key_size(varint) + child_page_id(4) - minimum 1 byte for size

	// Backward compatibility aliases
	LeafCellHeaderSize     = LeafCellHeaderSizeV1
	InternalCellHeaderSize = InternalCellHeaderSizeV1
)

var (
	ErrPageFull     = errors.New("page is full")
	ErrCellNotFound = errors.New("cell not found")
)

// Page represents a fixed 4KB block storing tree data
// Layout:
//   [Header: 8 bytes]
//   [Cell Directory: 2 bytes × num_cells]
//   [Free Space]
//   [Cells: growing backward from end]
type Page struct {
	id       uint32
	data     [PageSize]byte
	pageType byte
	dirty    bool
}

// NewPage creates a new page with the specified type
func NewPage(id uint32, pageType byte) *Page {
	p := &Page{
		id:       id,
		pageType: pageType,
		dirty:    true,
	}
	// Initialize header
	p.data[HeaderOffsetType] = pageType
	binary.BigEndian.PutUint16(p.data[HeaderOffsetNumCells:], 0)
	binary.BigEndian.PutUint32(p.data[HeaderOffsetRightPtr:], 0)
	binary.BigEndian.PutUint16(p.data[HeaderOffsetFreePtr:], PageSize)
	p.data[HeaderOffsetVersion] = PageFormatV2 // Use new varint format by default
	return p
}

// LoadPage loads a page from raw bytes
func LoadPage(id uint32, data []byte) (*Page, error) {
	if len(data) != PageSize {
		return nil, errors.New("invalid page size")
	}
	p := &Page{
		id:    id,
		dirty: false,
	}
	copy(p.data[:], data)
	p.pageType = p.data[HeaderOffsetType]
	return p, nil
}

// ID returns the page ID
func (p *Page) ID() uint32 {
	return p.id
}

// Type returns the page type
func (p *Page) Type() byte {
	return p.pageType
}

// Version returns the page format version
func (p *Page) Version() byte {
	version := p.data[HeaderOffsetVersion]
	if version == 0 {
		// Backward compatibility: pages without version are V1
		return PageFormatV1
	}
	return version
}

// IsLeaf returns true if this is a leaf page
func (p *Page) IsLeaf() bool {
	return p.pageType == PageTypeLeaf
}

// IsDirty returns true if the page has been modified
func (p *Page) IsDirty() bool {
	return p.dirty
}

// SetDirty marks the page as modified
func (p *Page) SetDirty(dirty bool) {
	p.dirty = dirty
}

// NumCells returns the number of cells in the page
func (p *Page) NumCells() uint16 {
	return binary.BigEndian.Uint16(p.data[HeaderOffsetNumCells:])
}

// setNumCells sets the number of cells
func (p *Page) setNumCells(n uint16) {
	binary.BigEndian.PutUint16(p.data[HeaderOffsetNumCells:], n)
}

// RightPtr returns the right pointer (for internal nodes and leaf linking)
func (p *Page) RightPtr() uint32 {
	return binary.BigEndian.Uint32(p.data[HeaderOffsetRightPtr:])
}

// SetRightPtr sets the right pointer
func (p *Page) SetRightPtr(ptr uint32) {
	binary.BigEndian.PutUint32(p.data[HeaderOffsetRightPtr:], ptr)
	p.dirty = true
}

// freePtr returns the offset where the next cell should be written
func (p *Page) freePtr() uint16 {
	return binary.BigEndian.Uint16(p.data[HeaderOffsetFreePtr:])
}

// setFreePtr sets the free pointer
func (p *Page) setFreePtr(ptr uint16) {
	binary.BigEndian.PutUint16(p.data[HeaderOffsetFreePtr:], ptr)
}

// Cell represents a single key-value pair or key-pointer pair
type Cell struct {
	Key   []byte
	Value []byte // For leaf nodes
	Child uint32 // For internal nodes (page ID)
}

// cellDirOffset returns the offset of the nth cell directory entry
func (p *Page) cellDirOffset(n uint16) int {
	return HeaderSize + int(n)*CellDirEntrySize
}

// getCellOffset returns the offset of the nth cell
func (p *Page) getCellOffset(n uint16) uint16 {
	offset := p.cellDirOffset(n)
	return binary.BigEndian.Uint16(p.data[offset:])
}

// setCellOffset sets the offset of the nth cell in the directory
func (p *Page) setCellOffset(n uint16, offset uint16) {
	dirOffset := p.cellDirOffset(n)
	binary.BigEndian.PutUint16(p.data[dirOffset:], offset)
}

// CellAt returns the cell at the specified index
func (p *Page) CellAt(index uint16) (*Cell, error) {
	if index >= p.NumCells() {
		return nil, ErrCellNotFound
	}

	offset := p.getCellOffset(index)
	if p.IsLeaf() {
		return p.parseLeafCell(int(offset))
	}
	return p.parseInternalCell(int(offset))
}

// parseLeafCell parses a leaf cell at the given offset
func (p *Page) parseLeafCell(offset int) (*Cell, error) {
	version := p.Version()

	if version == PageFormatV1 {
		// V1: Fixed 2-byte encoding
		if offset+LeafCellHeaderSizeV1 > PageSize {
			return nil, errors.New("invalid cell offset")
		}

		keySize := binary.BigEndian.Uint16(p.data[offset:])
		valueSize := binary.BigEndian.Uint16(p.data[offset+2:])

		if offset+LeafCellHeaderSizeV1+int(keySize)+int(valueSize) > PageSize {
			return nil, errors.New("invalid cell size")
		}

		cell := &Cell{
			Key:   make([]byte, keySize),
			Value: make([]byte, valueSize),
		}

		keyStart := offset + LeafCellHeaderSizeV1
		copy(cell.Key, p.data[keyStart:keyStart+int(keySize)])
		copy(cell.Value, p.data[keyStart+int(keySize):keyStart+int(keySize)+int(valueSize)])

		return cell, nil
	}

	// V2: Variable-length encoding
	if offset+LeafCellHeaderSizeV2Min > PageSize {
		return nil, errors.New("invalid cell offset")
	}

	// Decode key size (varint)
	keySize, n1 := uvarint16(p.data[offset:])
	if n1 <= 0 {
		return nil, errors.New("invalid key size varint")
	}

	// Decode value size (varint)
	valueSize, n2 := uvarint16(p.data[offset+n1:])
	if n2 <= 0 {
		return nil, errors.New("invalid value size varint")
	}

	headerSize := n1 + n2
	if offset+headerSize+int(keySize)+int(valueSize) > PageSize {
		return nil, errors.New("invalid cell size")
	}

	cell := &Cell{
		Key:   make([]byte, keySize),
		Value: make([]byte, valueSize),
	}

	keyStart := offset + headerSize
	copy(cell.Key, p.data[keyStart:keyStart+int(keySize)])
	copy(cell.Value, p.data[keyStart+int(keySize):keyStart+int(keySize)+int(valueSize)])

	return cell, nil
}

// parseInternalCell parses an internal cell at the given offset
func (p *Page) parseInternalCell(offset int) (*Cell, error) {
	version := p.Version()

	if version == PageFormatV1 {
		// V1: Fixed 2-byte encoding
		if offset+InternalCellHeaderSizeV1 > PageSize {
			return nil, errors.New("invalid cell offset")
		}

		keySize := binary.BigEndian.Uint16(p.data[offset:])
		child := binary.BigEndian.Uint32(p.data[offset+2:])

		if offset+InternalCellHeaderSizeV1+int(keySize) > PageSize {
			return nil, errors.New("invalid cell size")
		}

		cell := &Cell{
			Key:   make([]byte, keySize),
			Child: child,
		}

		keyStart := offset + InternalCellHeaderSizeV1
		copy(cell.Key, p.data[keyStart:keyStart+int(keySize)])

		return cell, nil
	}

	// V2: Variable-length encoding
	if offset+InternalCellHeaderSizeV2Min > PageSize {
		return nil, errors.New("invalid cell offset")
	}

	// Decode key size (varint)
	keySize, n := uvarint16(p.data[offset:])
	if n <= 0 {
		return nil, errors.New("invalid key size varint")
	}

	// Child page ID is still 4 bytes (fixed)
	child := binary.BigEndian.Uint32(p.data[offset+n:])

	headerSize := n + 4
	if offset+headerSize+int(keySize) > PageSize {
		return nil, errors.New("invalid cell size")
	}

	cell := &Cell{
		Key:   make([]byte, keySize),
		Child: child,
	}

	keyStart := offset + headerSize
	copy(cell.Key, p.data[keyStart:keyStart+int(keySize)])

	return cell, nil
}

// cellSize returns the size of a cell (header + key + value)
func (p *Page) cellSize(keySize, valueSize int) int {
	version := p.Version()

	if p.IsLeaf() {
		if version == PageFormatV1 {
			return LeafCellHeaderSizeV1 + keySize + valueSize
		}
		// V2: varint encoding for both key and value sizes
		keySizeVarint := varintSize16(uint16(keySize))
		valueSizeVarint := varintSize16(uint16(valueSize))
		return keySizeVarint + valueSizeVarint + keySize + valueSize
	}

	// Internal node
	if version == PageFormatV1 {
		return InternalCellHeaderSizeV1 + keySize
	}
	// V2: varint encoding for key size + fixed 4 bytes for child page ID
	keySizeVarint := varintSize16(uint16(keySize))
	return keySizeVarint + 4 + keySize
}

// IsFull checks if the page can fit a new cell
func (p *Page) IsFull(keySize, valueSize int) bool {
	numCells := p.NumCells()
	cellDirectoryEnd := p.cellDirOffset(numCells + 1)
	cellSize := p.cellSize(keySize, valueSize)
	freeSpace := int(p.freePtr()) - cellDirectoryEnd

	return freeSpace < cellSize
}

// InsertCell inserts a cell at the appropriate position (maintains sort order)
func (p *Page) InsertCell(cell *Cell) error {
	keySize := len(cell.Key)
	valueSize := 0
	if p.IsLeaf() {
		valueSize = len(cell.Value)
	}

	if p.IsFull(keySize, valueSize) {
		return ErrPageFull
	}

	// Find insertion position using binary search
	numCells := p.NumCells()
	insertPos := p.searchCell(cell.Key)
	if insertPos < 0 {
		// Found exact match - update in place
		insertPos = -insertPos - 1
		return p.updateCell(uint16(insertPos), cell)
	}

	// Allocate space for the new cell (grows backward from end)
	cellSize := p.cellSize(keySize, valueSize)
	newFreePtr := p.freePtr() - uint16(cellSize)

	// Write the cell data
	if p.IsLeaf() {
		p.writeLeafCell(int(newFreePtr), cell)
	} else {
		p.writeInternalCell(int(newFreePtr), cell)
	}

	// Shift cell directory entries to make room
	for i := numCells; i > uint16(insertPos); i-- {
		offset := p.getCellOffset(i - 1)
		p.setCellOffset(i, offset)
	}

	// Insert new cell offset
	p.setCellOffset(uint16(insertPos), newFreePtr)
	p.setNumCells(numCells + 1)
	p.setFreePtr(newFreePtr)
	p.dirty = true

	return nil
}

// updateCell updates an existing cell at the given position
func (p *Page) updateCell(index uint16, cell *Cell) error {
	// For simplicity, we'll delete and re-insert
	// A more efficient implementation would update in-place if sizes match
	if err := p.DeleteCell(index); err != nil {
		return err
	}
	return p.InsertCell(cell)
}

// writeLeafCell writes a leaf cell at the specified offset
func (p *Page) writeLeafCell(offset int, cell *Cell) {
	version := p.Version()

	if version == PageFormatV1 {
		// V1: Fixed 2-byte encoding
		binary.BigEndian.PutUint16(p.data[offset:], uint16(len(cell.Key)))
		binary.BigEndian.PutUint16(p.data[offset+2:], uint16(len(cell.Value)))
		copy(p.data[offset+LeafCellHeaderSizeV1:], cell.Key)
		copy(p.data[offset+LeafCellHeaderSizeV1+len(cell.Key):], cell.Value)
		return
	}

	// V2: Variable-length encoding
	n1 := putUvarint16(p.data[offset:], uint16(len(cell.Key)))
	n2 := putUvarint16(p.data[offset+n1:], uint16(len(cell.Value)))
	headerSize := n1 + n2
	copy(p.data[offset+headerSize:], cell.Key)
	copy(p.data[offset+headerSize+len(cell.Key):], cell.Value)
}

// writeInternalCell writes an internal cell at the specified offset
func (p *Page) writeInternalCell(offset int, cell *Cell) {
	version := p.Version()

	if version == PageFormatV1 {
		// V1: Fixed 2-byte encoding
		binary.BigEndian.PutUint16(p.data[offset:], uint16(len(cell.Key)))
		binary.BigEndian.PutUint32(p.data[offset+2:], cell.Child)
		copy(p.data[offset+InternalCellHeaderSizeV1:], cell.Key)
		return
	}

	// V2: Variable-length encoding for key size, fixed for child page ID
	n := putUvarint16(p.data[offset:], uint16(len(cell.Key)))
	binary.BigEndian.PutUint32(p.data[offset+n:], cell.Child)
	headerSize := n + 4
	copy(p.data[offset+headerSize:], cell.Key)
}

// SearchCell performs binary search for a key
// Returns the index where the key should be inserted if not found (positive)
// Returns -(index+1) if the key is found (negative)
func (p *Page) searchCell(key []byte) int {
	numCells := int(p.NumCells())
	left, right := 0, numCells

	for left < right {
		mid := (left + right) / 2
		cell, err := p.CellAt(uint16(mid))
		if err != nil {
			return left // Error case, insert at current position
		}

		cmp := bytes.Compare(key, cell.Key)
		if cmp == 0 {
			return -(mid + 1) // Found exact match
		} else if cmp < 0 {
			right = mid
		} else {
			left = mid + 1
		}
	}

	return left // Not found, return insertion position
}

// DeleteCell removes a cell at the specified index
func (p *Page) DeleteCell(index uint16) error {
	numCells := p.NumCells()
	if index >= numCells {
		return ErrCellNotFound
	}

	// Shift cell directory entries
	for i := index; i < numCells-1; i++ {
		offset := p.getCellOffset(i + 1)
		p.setCellOffset(i, offset)
	}

	p.setNumCells(numCells - 1)
	p.dirty = true

	// Note: This doesn't reclaim the cell's space immediately
	// Full defragmentation would be needed for space reclamation
	// For B-tree, this is acceptable as splits handle space issues

	return nil
}

// Data returns the raw page data
func (p *Page) Data() []byte {
	return p.data[:]
}

// Clone creates a copy of the page
func (p *Page) Clone() *Page {
	clone := &Page{
		id:       p.id,
		pageType: p.pageType,
		dirty:    p.dirty,
	}
	copy(clone.data[:], p.data[:])
	return clone
}
