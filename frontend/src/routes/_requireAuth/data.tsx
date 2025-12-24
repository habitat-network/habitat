import { useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useState, useMemo } from 'react'

export const Route = createFileRoute('/_requireAuth/data')({
  component: DataDebugger,
})

interface FilterCriteria {
  [key: string]: string
}

function DataDebugger() {
  const { authManager } = Route.useRouteContext()
  const [selectedLexicon, setSelectedLexicon] = useState<string>('')
  const [isPrivate, setIsPrivate] = useState(false)
  const [repoDid, setRepoDid] = useState<string>('')
  const [filterText, setFilterText] = useState('')
  const [parsedFilters, setParsedFilters] = useState<FilterCriteria>({})

  // Parse filter text into key-value pairs
  const parseFilters = (text: string): FilterCriteria => {
    const filters: FilterCriteria = {}
    const parts = text.trim().split(/\s+/)
    
    for (const part of parts) {
      if (part.includes(':')) {
        const [key, ...valueParts] = part.split(':')
        const value = valueParts.join(':') // Handle values that might contain colons
        if (key && value) {
          filters[key] = value
        }
      }
    }
    
    return filters
  }

  // Update filters when filter text changes
  const handleFilterChange = (text: string) => {
    setFilterText(text)
    setParsedFilters(parseFilters(text))
  }

  // Fetch data based on selected lexicon, privacy setting, and optional repo DID
  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['data-debugger', selectedLexicon, isPrivate, repoDid],
    queryFn: async () => {
      if (!selectedLexicon) {
        return null
      }
      
      // Use the repo DID if provided, otherwise undefined (uses default)
      const repo = repoDid.trim() || undefined
      
      if (isPrivate) {
        return await authManager.client().listPrivateRecords(selectedLexicon, undefined, undefined, repo)
      } else {
        return await authManager.client().listRecords(selectedLexicon, undefined, undefined, repo)
      }
    },
    enabled: !!selectedLexicon,
    retry: 2,
  })

  // Filter records based on parsed filter criteria
  const filteredRecords = useMemo(() => {
    if (!data) return []
    
    const records = data.records
    
    if (Object.keys(parsedFilters).length === 0) {
      return records
    }

    return records.filter((record) => {
      // Check all filter criteria
      for (const [key, value] of Object.entries(parsedFilters)) {
        if (key === 'rkey') {
          // Extract rkey from URI
          const rkey = record.uri?.split('/').pop()
          if (!rkey || !rkey.includes(value)) {
            return false
          }
        } else {
          // Check top-level fields in the record value
          const recordValue = record.value as Record<string, unknown>
          const fieldValue = recordValue[key]
          
          if (fieldValue === undefined) {
            return false
          }
          
          // Convert to string for comparison
          const fieldStr = String(fieldValue)
          if (!fieldStr.includes(value)) {
            return false
          }
        }
      }
      
      return true
    })
  }, [data, parsedFilters])

  return (
    <div style={{ padding: '1.5rem' }}>
      <h1 style={{ fontSize: '1.5rem', fontWeight: 'bold', marginBottom: '1rem' }}>Data Debugger</h1>

      {/* Top bar with controls */}
      <div style={{
        display: 'flex',
        flexWrap: 'wrap',
        alignItems: 'center',
        gap: '1rem',
        padding: '0.75rem 1rem',
        borderRadius: '6px',
        marginBottom: '1.5rem',
        border: '1px solid ButtonBorder',
        colorScheme: 'light dark'
      }}>
        {/* Lexicon Input */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <label htmlFor="lexicon" style={{ fontWeight: 500, color: 'CanvasText' }}>
            Lexicon:
          </label>
          <input
            id="lexicon"
            type="text"
            value={selectedLexicon}
            onChange={(e) => setSelectedLexicon(e.target.value)}
            placeholder="e.g., app.bsky.feed.post"
            style={{
              border: '1px solid ButtonBorder',
              borderRadius: '4px',
              backgroundColor: 'Field',
              color: 'FieldText',
              fontSize: '0.8rem',
              width: '280px'
            }}
          />
        </div>

        {/* Repo DID Input */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <label htmlFor="repoDid" style={{ fontWeight: 500, color: 'CanvasText' }}>
            DID:
          </label>
          <input
            id="repoDid"
            type="text"
            value={repoDid}
            onChange={(e) => setRepoDid(e.target.value)}
            placeholder="did:plc:..."
            style={{
              border: '1px solid ButtonBorder',
              borderRadius: '4px',
              fontFamily: 'monospace',
              backgroundColor: 'Field',
              color: 'FieldText',
              fontSize: '0.8rem',
              width: '280px'
            }}
          />
        </div>

        {/* Filter Input */}
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <label htmlFor="filter" style={{ fontWeight: 500, color: 'CanvasText' }}>
            Filter:
          </label>
          <input
            id="filter"
            type="text"
            value={filterText}
            onChange={(e) => handleFilterChange(e.target.value)}
            placeholder="key:value"
            style={{
              padding: '0.375rem 0.5rem',
              border: '1px solid ButtonBorder',
              borderRadius: '4px',
              backgroundColor: 'Field',
              color: 'FieldText',
              fontSize: '0.8rem'
            }}
          />
        </div>

        {/* Privacy Checkbox */}
        <label style={{ display: 'flex', alignItems: 'center', gap: '0.375rem', cursor: 'pointer', color: 'CanvasText' }}>
          <input
            type="checkbox"
            checked={isPrivate}
            onChange={(e) => setIsPrivate(e.target.checked)}
          />
          <span style={{ fontWeight: 500 }}>Private Data</span>
        </label>

        {/* Refresh Button */}
        <button
          onClick={() => refetch()}
          style={{
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            padding: '0.25rem',
            fontSize: '0.8rem',
            color: 'GrayText',
            textDecoration: 'underline'
          }}
        >
          Refresh
        </button>

        {/* Show active filters */}
        {Object.keys(parsedFilters).length > 0 && (
          <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginLeft: 'auto' }}>
            <span style={{ color: 'GrayText', fontSize: '0.875rem' }}>Active:</span>
            {Object.entries(parsedFilters).map(([key, value]) => (
              <span
                key={key}
                style={{
                  backgroundColor: 'Highlight',
                  color: 'HighlightText',
                  padding: '0.125rem 0.5rem',
                  borderRadius: '4px',
                  fontSize: '0.875rem'
                }}
              >
                <span style={{ fontWeight: 500 }}>{key}:</span>
                <span>{value}</span>
              </span>
            ))}
          </div>
        )}
      </div>

      {/* Data Display */}
      <div>
        {!selectedLexicon && (
          <div style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            padding: '4rem 2rem',
            color: 'GrayText',
            border: '2px dashed GrayText',
            borderRadius: '8px',
            opacity: 0.7
          }}>
            <div style={{ fontSize: '3rem', marginBottom: '1rem' }}>🔍</div>
            <h3 style={{ margin: 0, fontSize: '1.25rem', fontWeight: 500 }}>Enter a Lexicon</h3>
            <p style={{ margin: '0.5rem 0 0', fontSize: '0.875rem' }}>
              Type a lexicon (e.g., <code style={{ backgroundColor: 'ButtonFace', padding: '0.125rem 0.375rem', borderRadius: '3px' }}>app.bsky.feed.post</code>) to view records
            </p>
          </div>
        )}

        {isLoading && selectedLexicon && (
          <div>
            <div></div>
            <p>Loading records...</p>
          </div>
        )}

        {error && selectedLexicon && (
          <div>
            <div>
              <div>⚠️</div>
              <div>
                <h3>Error loading records</h3>
                <p>
                  {error instanceof Error ? error.message : 'An unknown error occurred'}
                </p>
                <button
                  onClick={() => refetch()}
                >
                  Try again
                </button>
              </div>
            </div>
          </div>
        )}

        {data && !isLoading && selectedLexicon && (
          <>
            <div style={{ marginBottom: '0.75rem', color: 'GrayText', fontSize: '0.875rem' }}>
              {filteredRecords.length} of {data.records?.length || 0} record(s)
              {Object.keys(parsedFilters).length > 0 && ' (filtered)'}
            </div>

            {filteredRecords && filteredRecords.length > 0 ? (
              <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
                {filteredRecords.map((record) => (
                  <div
                    key={record.uri}
                    style={{
                      border: '1px solid rgba(128, 128, 128, 0.25)',
                      borderRadius: '8px',
                      padding: '1rem',
                      backgroundColor: 'Canvas',
                      colorScheme: 'light dark'
                    }}
                  >
                    <div style={{
                      fontSize: '0.75rem',
                      fontFamily: 'monospace',
                      color: 'GrayText',
                      marginBottom: '0.75rem',
                      wordBreak: 'break-all'
                    }}>
                      {record.uri}
                    </div>
                    <pre style={{
                      margin: 0,
                      padding: '0.75rem',
                      backgroundColor: 'Field',
                      border: '1px solid ButtonBorder',
                      borderRadius: '4px',
                      fontFamily: 'monospace',
                      fontSize: '0.8rem',
                      overflow: 'auto',
                      whiteSpace: 'pre-wrap',
                      wordBreak: 'break-word',
                      color: 'FieldText'
                    }}>
{JSON.stringify(record.value, null, 2)}
                    </pre>
                  </div>
                ))}
              </div>
            ) : (
              <div>
                <div>📭</div>
                <h3>
                  {Object.keys(parsedFilters).length > 0 ? 'No matching records' : 'No records found'}
                </h3>
                <p>
                  {Object.keys(parsedFilters).length > 0
                    ? 'Try adjusting your filters'
                    : `No records found for collection "${selectedLexicon}"`}
                </p>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  )
}
