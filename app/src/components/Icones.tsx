interface IconProps {
  size?: number
}

function base(size?: number) {
  const s = size ?? 16
  return {
    width: s,
    height: s,
    viewBox: '0 0 24 24',
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.8,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
  }
}

export function IconeBusca({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.8-3.8" />
    </svg>
  )
}

export function IconeUpload({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="M12 16V4" />
      <path d="m6 10 6-6 6 6" />
      <path d="M4 20h16" />
    </svg>
  )
}

export function IconeArquivo({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
      <path d="M14 3v5h5" />
      <path d="M9 13h6" />
      <path d="M9 17h4" />
    </svg>
  )
}

export function IconeLixeira({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="M4 7h16" />
      <path d="M9 7V5a2 2 0 0 1 2-2h2a2 2 0 0 1 2 2v2" />
      <path d="M6 7l1 13a2 2 0 0 0 2 2h6a2 2 0 0 0 2-2l1-13" />
      <path d="M10 11v6" />
      <path d="M14 11v6" />
    </svg>
  )
}

export function IconeDownload({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="M12 4v12" />
      <path d="m7 11 5 5 5-5" />
      <path d="M4 20h16" />
    </svg>
  )
}

export function IconeSol({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 3v2" />
      <path d="M12 19v2" />
      <path d="m5 5 1.4 1.4" />
      <path d="m17.6 17.6 1.4 1.4" />
      <path d="M3 12h2" />
      <path d="M19 12h2" />
      <path d="m5 19 1.4-1.4" />
      <path d="m17.6 6.4 1.4-1.4" />
    </svg>
  )
}

export function IconeLua({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="M20 13.5A8 8 0 0 1 10.5 4 8 8 0 1 0 20 13.5z" />
    </svg>
  )
}

export function IconeVoltar({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="M15 5l-7 7 7 7" />
    </svg>
  )
}

export function IconeNota({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" />
      <path d="M14 3v5h5" />
      <path d="M8 14h8" />
      <path d="M8 17h5" />
    </svg>
  )
}

export function IconeHistorico({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <circle cx="12" cy="12" r="8.5" />
      <path d="M12 8v4l2.5 2.5" />
    </svg>
  )
}

export function IconeFechar({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="m6 6 12 12" />
      <path d="m18 6-12 12" />
    </svg>
  )
}

export function IconeSetaEsquerda({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="m14.5 6-6 6 6 6" />
    </svg>
  )
}

export function IconeSetaDireita({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="m9.5 6 6 6-6 6" />
    </svg>
  )
}

export function IconeLink({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="M10 13a5 5 0 0 0 7.54.54l2.1-2.1a5 5 0 0 0-7.07-7.07l-1.32 1.32" />
      <path d="M14 11a5 5 0 0 0-7.54-.54l-2.1 2.1a5 5 0 0 0 7.07 7.07l1.32-1.32" />
    </svg>
  )
}

export function IconeLivro({ size }: IconProps) {
  return (
    <svg {...base(size)}>
      <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20" />
      <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z" />
      <path d="M8 7h8" />
      <path d="M8 11h8" />
    </svg>
  )
}
