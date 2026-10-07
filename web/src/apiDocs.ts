// API reference page (/api/docs): Swagger UI over /api/openapi.json. It runs
// with the member's session, so "Try it out" acts as that member.
import SwaggerUIBundle from 'swagger-ui-dist/swagger-ui-es-bundle.js'
import 'swagger-ui-dist/swagger-ui.css'
import './apiDocs.css'

SwaggerUIBundle({
  url: '/api/openapi.json',
  dom_id: '#swagger-ui',
  deepLinking: true,
  displayOperationId: false,
  docExpansion: 'none',
  tryItOutEnabled: false,
  // Same-origin requests carry the session cookie; the server's CSRF check
  // needs the Origin header, which browsers send for these requests.
  withCredentials: true
})
