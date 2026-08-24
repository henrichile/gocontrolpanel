// Set mínimo de iconos de línea (trazo, sin relleno) para el panel. Se
// dibujan a mano en vez de traer una librería para no sumar una dependencia
// por un puñado de glifos — todos comparten el mismo trazo y proporción, así
// que se leen como una sola familia.
import type { SVGProps } from 'react'

export type IconName =
  | 'hard-drive'
  | 'server'
  | 'database'
  | 'cpu'
  | 'activity'
  | 'plus'
  | 'trash'
  | 'power'
  | 'stop'
  | 'refresh'
  | 'rotate'
  | 'redeploy'
  | 'globe'
  | 'folder'
  | 'file'
  | 'upload'
  | 'download'
  | 'archive'
  | 'edit'
  | 'home'
  | 'image'
  | 'chevron-right'
  | 'x'
  | 'save'
  | 'users'
  | 'user'
  | 'shield'
  | 'sliders'
  | 'tag'
  | 'lock'
  | 'search'
  | 'alert-triangle'
  | 'check-circle'

const PATHS: Record<IconName, string> = {
  'hard-drive':
    'M3 12h18M6.5 16h.01M10 16h4M5 5h14a2 2 0 0 1 2 2v6.5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2Z',
  server:
    'M4 4h16v6H4V4Zm0 10h16v6H4v-6Zm4 3h.01M8 7h.01',
  database:
    'M12 5c4.42 0 8-1.34 8-3s-3.58-3-8-3-8 1.34-8 3 3.58 3 8 3Zm-8-3v14c0 1.66 3.58 3 8 3s8-1.34 8-3V2M4 9c0 1.66 3.58 3 8 3s8-1.34 8-3',
  cpu:
    'M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3M7 7h10v10H7V7Zm3 3h4v4h-4v-4Z',
  activity: 'M22 12h-4l-3 8-6-16-3 8H2',
  plus: 'M12 5v14M5 12h14',
  trash: 'M4 7h16M9 7V4h6v3m-8 0 1 13h8l1-13M10 11v6M14 11v6',
  power: 'M12 2v8M18.4 6.6a8 8 0 1 1-12.8 0',
  stop: 'M7 4h10a3 3 0 0 1 3 3v10a3 3 0 0 1-3 3H7a3 3 0 0 1-3-3V7a3 3 0 0 1 3-3Z',
  refresh: 'M21 12a9 9 0 1 1-3-6.7M21 3v6h-6',
  rotate: 'M3 12a9 9 0 1 0 3-6.7M3 3v6h6',
  redeploy: 'M12 3v6l4-3-4-3ZM12 21v-6l-4 3 4 3ZM3 12h6M15 12h6',
  globe:
    'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18Zm-9-9h18M12 3a13 13 0 0 1 0 18 13 13 0 0 1 0-18Z',
  folder: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z',
  file: 'M6 3h8l5 5v12a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1Zm8 0v5h5',
  upload: 'M12 20V6M6 12l6-6 6 6M4 20h16',
  download: 'M12 4v14M6 12l6 6 6-6M4 20h16',
  archive: 'M4 4h16v4H4V4Zm1 4h14v12H5V8Zm5 3h4v3h-4v-3Z',
  edit: 'M4 20h4L18.5 9.5a2.1 2.1 0 0 0-3-3L5 17v3Zm10.5-14.5 3 3',
  home: 'M3 11.5 12 4l9 7.5M5.5 10v9a1 1 0 0 0 1 1h4v-6h3v6h4a1 1 0 0 0 1-1v-9',
  image: 'M4 5h16a1 1 0 0 1 1 1v12a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V6a1 1 0 0 1 1-1Zm2 12 4.5-5.5 3 3.5L17 10l4 7H6Z',
  'chevron-right': 'm9 6 6 6-6 6',
  x: 'M18 6 6 18M6 6l12 12',
  save: 'M5 4h11l3 3v13a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1Zm3 0v5h8V4M8 13h8v7H8v-7Z',
  users:
    'M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2M9 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8ZM23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75',
  user: 'M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2M12 11a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z',
  shield: 'M12 2 4 5v6c0 5 3.5 9 8 11 4.5-2 8-6 8-11V5l-8-3Z',
  sliders: 'M4 21v-7M4 10V3M12 21v-9M12 8V3M20 21v-5M20 12V3M1 14h6M9 8h6M17 16h6',
  tag: 'M20.59 13.41 13.41 20.59a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82ZM7 7h.01',
  lock: 'M19 11H5a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7a2 2 0 0 0-2-2ZM7 11V7a5 5 0 0 1 10 0v4',
  search: 'M11 19a8 8 0 1 0 0-16 8 8 0 0 0 0 16ZM21 21l-4.35-4.35',
  'alert-triangle':
    'M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0ZM12 9v4M12 17h.01',
  'check-circle': 'M22 11.08V12a10 10 0 1 1-5.93-9.14M22 4 12 14.01l-3-3',
}

export function Icon({
  name,
  size = 16,
  strokeWidth = 1.75,
  ...rest
}: { name: IconName; size?: number } & SVGProps<SVGSVGElement>) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      {...rest}
    >
      <path d={PATHS[name]} />
    </svg>
  )
}
