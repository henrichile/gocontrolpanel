// Empaqueta Monaco localmente (vía Vite) en vez de dejar que
// @monaco-editor/react lo baje de un CDN externo en tiempo de ejecución: este
// panel no debería depender de terceros ni filtrar su stack para algo tan
// básico como abrir un editor de texto.
import { loader } from '@monaco-editor/react'
import * as monaco from 'monaco-editor'
import editorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import cssWorker from 'monaco-editor/esm/vs/language/css/css.worker?worker'
import htmlWorker from 'monaco-editor/esm/vs/language/html/html.worker?worker'
import jsonWorker from 'monaco-editor/esm/vs/language/json/json.worker?worker'
import tsWorker from 'monaco-editor/esm/vs/language/typescript/ts.worker?worker'

self.MonacoEnvironment = {
  getWorker(_workerId: string, label: string) {
    switch (label) {
      case 'json': return new jsonWorker()
      case 'css': case 'scss': case 'less': return new cssWorker()
      case 'html': case 'handlebars': case 'razor': return new htmlWorker()
      case 'typescript': case 'javascript': return new tsWorker()
      default: return new editorWorker()
    }
  },
}

loader.config({ monaco })
