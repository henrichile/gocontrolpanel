<#
.SYNOPSIS
	Elimina los ficheros «:Zone.Identifier» que Windows deja al copiar archivos
	descargados hacia WSL o hacia un recurso compartido.

.DESCRIPTION
	Recorre recursivamente las rutas indicadas (por defecto el directorio actual)
	y borra todo fichero cuyo nombre termine en «Zone.Identifier». Se omiten
	.git, node_modules y vendor.

	En montajes WSL/drvfs los dos puntos del nombre se almacenan como U+F03A, por
	lo que Remove-Item puede interpretarlos como un flujo alternativo (ADS). Por
	eso el borrado se hace con [System.IO.File]::Delete(), que actúa siempre
	sobre el fichero real.

.PARAMETER Path
	Una o varias rutas a limpiar. Por defecto, el directorio actual.

.PARAMETER DryRun
	Muestra qué se borraría sin tocar el disco.

.PARAMETER Quiet
	No lista los ficheros, solo el total.

.EXAMPLE
	.\scripts\clean-zone-identifier.ps1
.EXAMPLE
	.\scripts\clean-zone-identifier.ps1 -Path Z:\home\Proyectos -DryRun
#>
[CmdletBinding()]
param(
	[Parameter(Position = 0, ValueFromRemainingArguments = $true)]
	[string[]]$Path = @('.'),

	[switch]$DryRun,
	[switch]$Quiet
)

$ErrorActionPreference = 'Stop'
$excluded = @('.git', 'node_modules', 'vendor')
$total = 0

foreach ($root in $Path) {
	if (-not (Test-Path -LiteralPath $root -PathType Container)) {
		Write-Error "No es un directorio: $root"
		exit 1
	}

	$full = (Resolve-Path -LiteralPath $root).ProviderPath

	$files = Get-ChildItem -LiteralPath $full -Recurse -Force -File -ErrorAction SilentlyContinue |
		Where-Object { $_.Name -like '*Zone.Identifier' } |
		Where-Object {
			$rel = $_.FullName.Substring($full.Length).Trim([char]'\', [char]'/')
			$parts = $rel -split '[\\/]'
			-not ($parts | Where-Object { $excluded -contains $_ })
		}

	foreach ($file in $files) {
		$total++
		if (-not $Quiet) { Write-Host $file.FullName }
		if (-not $DryRun) { [System.IO.File]::Delete($file.FullName) }
	}
}

if ($DryRun) {
	Write-Host "→ $total fichero(s) Zone.Identifier encontrados (simulacro, no se ha borrado nada)"
} else {
	Write-Host "→ $total fichero(s) Zone.Identifier eliminados"
}
