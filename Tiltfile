LOCAL_REGISTRY = os.getenv('LOCAL_REGISTRY')
LAB_HOST = os.getenv('LAB_HOST')
BUILD_IMAGE_SKIP = os.getenv('BUILD_IMAGE_SKIP', "0")

SERVICE_NAME = 'ai-speech-ingress-service'
IMAGE = LOCAL_REGISTRY + '/' + SERVICE_NAME

cpu_arch = str(local(
  "uname -m",
  command_bat="echo %PROCESSOR_ARCHITECTURE%",
  quiet=True
)).strip()

if cpu_arch in ["aarch64", "ARM64", "arm64"]:
  arch = "aarch64"
else:
  arch = cpu_arch

print("Detected architecture:", arch)

if (BUILD_IMAGE_SKIP == "0"):
  git_commit = str(local('git describe --always')).strip()

  docker_build(
    IMAGE,
    context='.',
    dockerfile='docker/Dockerfile',
    build_args={'ARCH': arch},
  )

if (BUILD_IMAGE_SKIP == "1"):
  git_commit = "test"

helm_file = helm(
  './helm/' + SERVICE_NAME,
  SERVICE_NAME,
  'core',
  set=[
    'image.repository=' + IMAGE,
    'image.tag=' + git_commit,
  ]
)

k8s_yaml(helm_file)

