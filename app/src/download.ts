const EXTENSOES_POR_MIME: Record<string, string> = {
  'application/pdf': 'pdf',
  'application/json': 'json',
  'application/msword': 'doc',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'docx',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': 'xlsx',
  'application/vnd.openxmlformats-officedocument.presentationml.presentation': 'pptx',
  'application/zip': 'zip',
  'text/csv': 'csv',
  'text/plain': 'txt',
  'image/jpeg': 'jpg',
  'image/png': 'png',
  'image/webp': 'webp',
}

function extensaoParaMime(tipoMime?: string | null): string {
  const tipoNormalizado = tipoMime?.split(';', 1)[0]?.trim().toLowerCase()
  return tipoNormalizado ? EXTENSOES_POR_MIME[tipoNormalizado] ?? '' : ''
}

function limparNomeArquivo(valor?: string | null): string {
  const ultimoSegmento = valor?.split(/[\\/]+/).pop() ?? ''
  const semCaracteresDeControle = ultimoSegmento.replace(/[\u0000-\u001f\u007f]/g, '')
  return semCaracteresDeControle
    .replace(/[<>:"|?*]/g, '-')
    .replace(/\s+/g, ' ')
    .replace(/^[.\s]+|[.\s]+$/g, '')
}

function temExtensao(nome: string): boolean {
  const ultimoPonto = nome.lastIndexOf('.')
  return ultimoPonto > 0 && ultimoPonto < nome.length - 1
}

/** Cria um nome seguro para downloads quando o servidor não informa filename. */
export function nomeArquivoParaDownload(
  nomeSugerido: string | null | undefined,
  tipoMime: string | null | undefined,
  fallbackBase = 'download',
): string {
  const base = limparNomeArquivo(nomeSugerido) || limparNomeArquivo(fallbackBase) || 'download'
  const extensao = extensaoParaMime(tipoMime)
  return extensao && !temExtensao(base) ? `${base}.${extensao}` : base
}

/** Dispara o download de um Blob sem depender de metadados opcionais do arquivo. */
export function baixarBlob(
  blob: Blob,
  nomeSugerido?: string | null,
  fallbackBase?: string,
): string {
  const nome = nomeArquivoParaDownload(nomeSugerido, blob.type, fallbackBase)
  if (typeof document === 'undefined') throw new Error('Download indisponível fora do navegador')

  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = nome
  link.style.display = 'none'
  document.body.appendChild(link)
  link.click()
  link.remove()
  setTimeout(() => URL.revokeObjectURL(url), 0)
  return nome
}
