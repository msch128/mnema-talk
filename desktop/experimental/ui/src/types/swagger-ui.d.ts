// Minimal declaration of the bundled Swagger UI entrypoint used by apiDocs.ts.
// Swagger's build artifact remains externally maintained JavaScript.
declare module 'swagger-ui-dist/swagger-ui-es-bundle.js' {
  interface SwaggerUIOptions {
    url: string
    dom_id: string
    deepLinking?: boolean
    displayOperationId?: boolean
    docExpansion?: 'none' | 'list' | 'full'
    tryItOutEnabled?: boolean
    withCredentials?: boolean
  }
  export default function SwaggerUI(options: SwaggerUIOptions): unknown
}
