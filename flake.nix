{
  inputs = {
    nixpkgs.url = "github:amarbel-llc/nixpkgs";
    nixpkgs-master.url = "github:NixOS/nixpkgs/e2dde111aea2c0699531dc616112a96cd55ab8b5";
    utils.url = "https://flakehub.com/f/numtide/flake-utils/0.1.102";

    tommy = {
      url = "github:amarbel-llc/tommy";
      inputs.utils.follows = "utils";
    };

    bob = {
      url = "github:amarbel-llc/bob";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };

    purse-first = {
      url = "github:amarbel-llc/purse-first";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.nixpkgs-master.follows = "nixpkgs-master";
      inputs.utils.follows = "utils";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      nixpkgs-master,
      utils,
      tommy,
      bob,
      purse-first,
      ...
    }:
    let
      # Burnt into the binary via the fork's auto-injected -ldflags.
      # Single source of truth for the release version; `just bump-version`
      # sed-rewrites this line.
      madderVersion = "0.2.2";
      # shortRev for clean builds, dirtyShortRev for dirty working trees
      # (so devshell builds show `dirty-abcdef` rather than masquerading
      # as a clean release), "unknown" as a last-resort fallback.
      madderCommit = self.shortRev or self.dirtyShortRev or "unknown";
    in
    (utils.lib.eachDefaultSystem (
      system:
      let
        result = import ./go/default.nix {
          inherit
            nixpkgs
            nixpkgs-master
            tommy
            bob
            purse-first
            system
            ;
          man7Src = ./docs/man.7;
          version = madderVersion;
          commit = madderCommit;
        };
      in
      {
        inherit (result) packages;
        devShells.default = result.devShells.default;
      }
    ));
}
