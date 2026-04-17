{
  nixpkgs,
  nixpkgs-master,
  tommy,
  gomod2nix,
  bob,
  system,
  man7Src ? null,
}:
let
  pkgs = import nixpkgs {
    inherit system;
    overlays = [
      gomod2nix.overlays.default
    ];
  };

  pkgs-master = import nixpkgs-master {
    inherit system;
  };

  madder = pkgs.buildGoApplication {
    pname = "madder";
    version = "0.0.1";
    src = ./.;
    pwd = ./.;
    subPackages = [
      "cmd/madder"
      "cmd/madder-gen_man"
    ];
    modules = ./gomod2nix.toml;
    go = pkgs-master.go_1_26;
    GOTOOLCHAIN = "local";

    nativeBuildInputs = pkgs-master.lib.optionals (man7Src != null) [
      pkgs-master.pandoc
    ];

    # madder-gen_man takes a *prefix* and writes to {prefix}/share/man/man1/
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
      gomod2nix.packages.${system}.default
      tommy.packages.${system}.default
      bob.packages.${system}.batman
    ]
    ++ (with pkgs-master; [
      delve
      go_1_26
      gofumpt
      gopls
      gotools
      pandoc
      shfmt
    ]);

    GOTOOLCHAIN = "local";
  };
}
