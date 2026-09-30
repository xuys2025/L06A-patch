PACKAGE_NAME="core_api"
PACKAGE_VERSION="1.0.0"
PACKAGE_DEPENDS="python3"

STAGING_PYTHON=`find ${STAGING_DIR}/usr/lib -maxdepth 1 -type d -name 'python3.*' -print -quit`

configure_package() {
	# Resolve dependencies for the Python version shipped in the target image.
	# The build host runs a newer Python, so an unconstrained pip invocation can
	# otherwise select packages which cannot be imported by target Python 3.9.
	rm -rf "${STAGING_PYTHON}/site-packages"
	mkdir -p "${STAGING_PYTHON}/site-packages"

	pip3 install --no-cache-dir \
		--target="${STAGING_PYTHON}/site-packages" \
		--python-version=3.9 \
		--implementation=cp \
		--abi=cp39 \
		--platform=manylinux2014_x86_64 \
		--only-binary=:all: \
		'Flask>=3' \
		'requests>2,<3' \
		'certifi>=2024' \
		'wyoming==1.6.0' \
		'APScheduler>=3.2.0,<4' \
		'python-dateutil>=2.4.2' \
		pyyaml

	# These two projects publish universal Python sources. Install without
	# resolving again so the Python 3.9-compatible dependency set above remains
	# authoritative.
	pip3 install --no-cache-dir --no-deps \
		--target="${STAGING_PYTHON}/site-packages" \
		Flask-APScheduler==1.13.1 \
		pyring-buffer
}

install_package() {
	rm -rf ${STAGING_DIR}/usr/share/api
	cp -rvf ${WORKSPACE_DIR}/../api ${STAGING_DIR}/usr/share/api

	cp -v ${PACKAGE_DIR}/config/api.init ${STAGING_DIR}/etc/init.d/api
	ln -sf ../init.d/api ${STAGING_DIR}/etc/rc.d/S98api
}

postinstall_package() {
	# Remove unused data and bytecode produced by the build-host interpreter.
	for NAME in pydoc_data ensurepip 'asyncio/windows_*.py' _osx_support.py test unitest __pycache__ ; do
		rm -rf ${STAGING_PYTHON}/${NAME}
	done

	find "${STAGING_PYTHON}" -type d -name '__pycache__' -prune -exec rm -rf {} \;

	for NAME in test idle_test tests ; do
		find "${STAGING_PYTHON}" -mindepth 2 -type d -name "${NAME}" -prune -exec rm -rf {} \;
	done

	# The selected wheels contain optional acceleration modules for the build
	# host. Their packages have pure-Python fallbacks, so remove every non-ARM
	# ELF extension and fail if one survives.
	while IFS= read -r -d '' MODULE ; do
		DESCRIPTION="$(file -b "${MODULE}")"
		case "${DESCRIPTION}" in
			*ELF*ARM*) ;;
			*ELF*)
				echo "Removing host Python extension: ${MODULE} (${DESCRIPTION})"
				rm -f "${MODULE}"
				;;
		esac
	done < <(find "${STAGING_PYTHON}/site-packages" -type f \( -name '*.so' -o -name '*.so.*' \) -print0)

	if find "${STAGING_PYTHON}/site-packages" -type f -print0 \
		| xargs -0r file \
		| grep -Eq 'ELF .*x86-64|ELF .*Intel 80386' ; then
		echo "Host-architecture ELF remains in target site-packages" >&2
		return 1
	fi

	if find "${STAGING_PYTHON}/site-packages" -type f -name '*cpython-314*' -print -quit \
		| grep -q . ; then
		echo "Python 3.14 build artifact remains in target site-packages" >&2
		return 1
	fi
}
