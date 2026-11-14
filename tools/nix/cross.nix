# sshwarm for another machine: Go cross-compiles by itself, so this machine builds
# them all. sshwarm's tests ran for this one.
{
  sshwarm,
  goos,
  goarch,
}:
sshwarm.overrideAttrs {
  pname = "sshwarm-${goos}-${goarch}";
  doCheck = false;
  dontFixup = true; # nothing to patch in a static binary, maybe for another machine
  buildPhase = ''
    runHook preBuild
    mkdir -p $out/bin
    GOOS=${goos} GOARCH=${goarch} go build -ldflags "-s -w" -o $out/bin/sshwarm .
    runHook postBuild
  '';
  installPhase = "true";
}
