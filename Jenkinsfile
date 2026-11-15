// Jenkins pipeline for the same checks as .github/workflows/ci.yml, for a
// Jenkins server with Docker. It tests inside a Go container, builds both
// images, and on main can trigger the Render deploy.
//
// To let main deploy: add a "Secret text" credential with id
// render-deploy-hook holding the service's Deploy Hook URL (Render dashboard
// -> service -> Settings). Without it the Deploy stage is skipped.
pipeline {
  agent any
  options { timeout(time: 20, unit: 'MINUTES'); disableConcurrentBuilds() }

  stages {
    stage('Test') {
      agent { docker { image 'golang:1.25-alpine'; reuseNode true; args '-e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath' } }
      steps {
        sh 'apk add --no-cache git >/dev/null'
        // cmd/tools holds one-off scripts that each declare main.
        sh 'go build ./cmd/server ./internal/...'
        sh 'go vet ./cmd/server ./internal/...'
        sh 'go test ./internal/... -count=1'
      }
    }

    stage('Docker images') {
      steps {
        sh 'docker build -t portal-ci:${BUILD_NUMBER} .'
        sh 'docker build -t n8n-ci:${BUILD_NUMBER} n8n'
      }
    }

    stage('Deploy') {
      when { branch 'main' }
      steps {
        script {
          try {
            withCredentials([string(credentialsId: 'render-deploy-hook', variable: 'HOOK')]) {
              sh 'curl -fsS -X POST "$HOOK"'
            }
          } catch (e) {
            echo 'No render-deploy-hook credential: Render deploys main by itself, skipping.'
          }
        }
      }
    }
  }

  post {
    always { sh 'docker rmi portal-ci:${BUILD_NUMBER} n8n-ci:${BUILD_NUMBER} || true' }
  }
}
