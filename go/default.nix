{
  self,
  nixpkgs,
  nixpkgs-master,
  tommy,
  bob,
  purse-first,
  system,
  man7Src ? null,
  version ? "dev",
}:
let
  pkgs = import nixpkgs { inherit system; };

  pkgs-master = import nixpkgs-master {
    inherit system;
  };

  madder = pkgs.buildGoApplication {
    pname = "madder";
    inherit version;
    commit = self.shortRev or self.dirtyShortRev or "unknown";
    src = self;
    pwd = ./.;
    subPackages = [
      "cmd/madder"
      "cmd/madder-cache"
      "cmd/madder-gen_man"
    ];
    modules = ./gomod2nix.toml;
    go = pkgs-master.go_1_26;
    GOTOOLCHAIN = "local";

    nativeBuildInputs = [
      purse-first.packages.${system}.dagnabit
    ] ++ pkgs-master.lib.optionals (man7Src != null) [
      pkgs-master.pandoc
    ];

    preBuild = ''
      dagnabit export
    '';

    postInstall = ''
      $out/bin/madder-gen_man $out
      rm $out/bin/madder-gen_man
    ''
    + pkgs-master.lib.optionalString (man7Src != null) ''
      mkdir -p $out/share/man/man7
      for f in ${man7Src}/*.md; do
        name="$(basename "$f" .md)"
        pandoc -s -t man "$f" -o "$out/share/man/man7/$name.7"
        ${pkgs-master.gnused}/bin/sed -i '3a\.\" Formatting overrides\n.ss 12 0\n.na' "$out/share/man/man7/$name.7"
      done
    '';
  };
in
{
  packages = {
    inherit madder;
    default = madder;
  };

  devShells.default = pkgs-master.mkShell {
    packages = [
      (pkgs.mkGoEnv { pwd = ./.; go = pkgs-master.go_1_26; })
      tommy.packages.${system}.default
      bob.packages.${system}.batman
      purse-first.packages.${system}.dagnabit
    ]
    ++ (with pkgs-master; [
      delve
      gofumpt
      gopls
      gotools
      just
      pandoc
      shfmt
    ]);

    GOTOOLCHAIN = "local";
  };
}
