// CI/CD pipeline, run by the Jenkins container in docker/jenkins (docker
// compose --profile ci up -d jenkins). It polls the repository every two
// minutes, tests main, builds both images, and then asks Render to deploy.
//
// Render also deploys main by itself when it sees a push. The Deploy Hook
// stage matters when that automatic deploy is switched off, so a commit ships
// only after the tests above it pass: put the service's Deploy Hook URL in
// RENDER_DEPLOY_HOOK in .env. Without it the stage is skipped.
pipeline {
  agent any
  options { timeout(time: 25, unit: 'MINUTES'); disableConcurrentBuilds(); timestamps() }

  stages {
    stage('Test') {
      steps {
        // cmd/tools holds one-off scripts that each declare main, so only the
        // server and internal/ are built as a set.
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
      when { expression { (env.GIT_BRANCH ?: '').endsWith('main') } }
      steps {
        withCredentials([string(credentialsId: 'render-deploy-hook', variable: 'HOOK')]) {
          sh '''
            if [ "$HOOK" = "none" ]; then
              echo "No Render deploy hook set: Render deploys main by itself."
            else
              curl -fsS -X POST "$HOOK" >/dev/null && echo "Render deploy triggered"
            fi
          '''
        }
      }
    }
  }

  post {
    always { sh 'docker rmi portal-ci:${BUILD_NUMBER} n8n-ci:${BUILD_NUMBER} || true' }
  }
}
