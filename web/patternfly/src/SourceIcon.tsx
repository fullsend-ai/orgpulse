const ICONS: Record<string, { svg: JSX.Element; color: string; label: string }> = {
  github: {
    label: 'GitHub',
    color: '#24292f',
    svg: (
      <svg viewBox="0 0 16 16" fill="currentColor">
        <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27s1.36.09 2 .27c1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0016 8c0-4.42-3.58-8-8-8z" />
      </svg>
    ),
  },
  gitlab: {
    label: 'GitLab',
    color: '#e24329',
    svg: (
      <svg viewBox="0 0 16 16" fill="currentColor">
        <path d="M8 14.86L10.59 6.7H5.41L8 14.86z" />
        <path d="M8 14.86L5.41 6.7H1.07L8 14.86z" opacity=".7" />
        <path d="M1.07 6.7L.12 9.63a.65.65 0 00.24.73L8 14.86 1.07 6.7z" opacity=".5" />
        <path d="M1.07 6.7h4.34L3.56 1.14a.33.33 0 00-.62 0L1.07 6.7z" />
        <path d="M8 14.86l2.59-8.16h4.34L8 14.86z" opacity=".7" />
        <path d="M14.93 6.7l.95 2.93a.65.65 0 01-.24.73L8 14.86l6.93-8.16z" opacity=".5" />
        <path d="M14.93 6.7H10.6l1.85-5.56a.33.33 0 01.62 0l1.86 5.56z" />
      </svg>
    ),
  },
  jira: {
    label: 'Jira',
    color: '#0052cc',
    svg: (
      <svg viewBox="0 0 16 16" fill="currentColor">
        <path d="M15.04 7.29L8.75.96 8 .21 2.67 5.54.96 7.29a.33.33 0 000 .46l4.58 4.58L8 14.79l5.33-5.33.24-.24 1.47-1.47a.33.33 0 000-.46zM8 10.13L5.87 8 8 5.87 10.13 8 8 10.13z" />
      </svg>
    ),
  },
  codecov: {
    label: 'Codecov',
    color: '#f01f7a',
    svg: (
      <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <circle cx="8" cy="8" r="7" stroke="currentColor" strokeWidth="2" />
        <path d="M10.5 5.5a3.5 3.5 0 1 0 0 5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      </svg>
    ),
  },
  testregistry: {
    label: 'TestRegistry',
    color: '#3e8635',
    svg: (
      <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
        <path d="M6 1.5h4M6.75 1.5v4.25L2.6 12.9A1.1 1.1 0 0 0 3.55 14.5h8.9a1.1 1.1 0 0 0 .95-1.6L9.25 5.75V1.5"
          stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        <path d="M4.4 10h7.2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
      </svg>
    ),
  },
}

export default function SourceIcon({ source, size = 18 }: { source: string; size?: number }) {
  const info = ICONS[source]
  if (!info) return <span>{source}</span>
  return (
    <span className="source-icon" style={{ color: info.color, width: size, height: size }} title={info.label}>
      {info.svg}
    </span>
  )
}

export function sourceLabel(source: string): string {
  return ICONS[source]?.label ?? source
}
